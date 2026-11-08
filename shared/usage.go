package shared

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

const (
	usageFlushInterval = 10 * time.Second
	usageFlushKeys     = 1000
	usageFlushTimeout  = 30 * time.Second
	usageFlushAttempts = 3
	usageFlushBackoff  = 100 * time.Millisecond
)

type usageKey struct {
	AppID      uuid.UUID
	UserID     uuid.UUID
	ObjectType string
	ObjectID   uuid.UUID
}

func (k usageKey) less(o usageKey) bool {
	if c := bytes.Compare(k.UserID[:], o.UserID[:]); c != 0 {
		return c < 0
	}
	if c := bytes.Compare(k.AppID[:], o.AppID[:]); c != 0 {
		return c < 0
	}
	if k.ObjectType != o.ObjectType {
		return k.ObjectType < o.ObjectType
	}
	return bytes.Compare(k.ObjectID[:], o.ObjectID[:]) < 0
}

type usagePost func(ctx context.Context, keys []usageKey, counts []int64) error

type ObjectUsage struct {
	mu      sync.Mutex
	counts  map[usageKey]int64
	limit   int
	post    usagePost
	report  func(error)
	backoff time.Duration
	full    chan struct{}
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
}

func startObjectUsage(post usagePost, report func(error), interval time.Duration, limit int) *ObjectUsage {
	u := &ObjectUsage{
		counts:  make(map[usageKey]int64),
		limit:   limit,
		post:    post,
		report:  report,
		backoff: usageFlushBackoff,
		full:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go u.run(interval)
	return u
}

func (u *ObjectUsage) Add(appID *uuid.UUID, userID uuid.UUID, objectType string, objectID uuid.UUID) {
	if u == nil || userID == uuid.Nil || objectID == uuid.Nil || objectType == "" {
		return
	}
	key := usageKey{UserID: userID, ObjectType: objectType, ObjectID: objectID}
	if appID != nil {
		key.AppID = *appID
	}
	u.mu.Lock()
	u.counts[key]++
	full := len(u.counts) == u.limit
	u.mu.Unlock()
	if full {
		select {
		case u.full <- struct{}{}:
		default:
		}
	}
}

func (u *ObjectUsage) Stop() {
	if u == nil {
		return
	}
	u.once.Do(func() { close(u.stop) })
	<-u.done
}

func (u *ObjectUsage) run(interval time.Duration) {
	defer close(u.done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			u.flush()
		case <-u.full:
			u.flush()
		case <-u.stop:
			u.flush()
			return
		}
	}
}

func (u *ObjectUsage) flush() {
	u.mu.Lock()
	counted := u.counts
	u.counts = make(map[usageKey]int64, len(counted))
	u.mu.Unlock()
	if len(counted) == 0 {
		return
	}
	keys := make([]usageKey, 0, len(counted))
	for k := range counted {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].less(keys[j]) })
	counts := make([]int64, len(keys))
	for i, k := range keys {
		counts[i] = counted[k]
	}
	var err error
	for attempt := 1; attempt <= usageFlushAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), usageFlushTimeout)
		err = u.post(ctx, keys, counts)
		cancel()
		if err == nil || !usageRetryable(err) || attempt == usageFlushAttempts {
			break
		}
		time.Sleep(time.Duration(attempt) * u.backoff)
	}
	if err == nil {
		return
	}
	u.mu.Lock()
	for i, k := range keys {
		u.counts[k] += counts[i]
	}
	u.mu.Unlock()
	if u.report != nil {
		u.report(err)
	}
}

func usageRetryable(err error) bool {
	var pgErr interface{ Field(byte) string }
	if !errors.As(err, &pgErr) {
		return false
	}
	switch pgErr.Field('C') {
	case "40P01", "40001":
		return true
	}
	return false
}

func (d *DB) postObjectUsage(ctx context.Context, keys []usageKey, counts []int64) error {
	var ownUsers, ownObjects, appUsers, appApps, appObjects []uuid.UUID
	var ownTypes, appTypes []string
	var ownCounts, appCounts []int64
	for i, k := range keys {
		if k.AppID == uuid.Nil {
			ownUsers = append(ownUsers, k.UserID)
			ownTypes = append(ownTypes, k.ObjectType)
			ownObjects = append(ownObjects, k.ObjectID)
			ownCounts = append(ownCounts, counts[i])
			continue
		}
		appUsers = append(appUsers, k.UserID)
		appApps = append(appApps, k.AppID)
		appTypes = append(appTypes, k.ObjectType)
		appObjects = append(appObjects, k.ObjectID)
		appCounts = append(appCounts, counts[i])
	}
	own := "SELECT claimius.touch_object_usage(?::uuid[], ?::text[], ?::uuid[], ?::bigint[])"
	apps := "SELECT claimius.touch_object_usage(?::uuid[], ?::uuid[], ?::text[], ?::uuid[], ?::bigint[])"
	ownArgs := []any{pq.Array(ownUsers), pq.Array(ownTypes), pq.Array(ownObjects), pq.Array(ownCounts)}
	appArgs := []any{pq.Array(appUsers), pq.Array(appApps), pq.Array(appTypes), pq.Array(appObjects), pq.Array(appCounts)}
	var err error
	switch {
	case len(appUsers) == 0:
		_, err = d.NewRaw(own, ownArgs...).Exec(ctx)
	case len(ownUsers) == 0:
		_, err = d.NewRaw(apps, appArgs...).Exec(ctx)
	default:
		_, err = d.NewRaw(own+" UNION ALL "+apps, append(ownArgs, appArgs...)...).Exec(ctx)
	}
	return err
}
