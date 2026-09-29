package shared

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

const (
	usageFlushInterval = 10 * time.Second
	usageFlushKeys     = 1000
	usageFlushTimeout  = 30 * time.Second
)

type usageKey struct {
	UserID     uuid.UUID
	ObjectType string
	ObjectID   uuid.UUID
}

type usagePost func(ctx context.Context, keys []usageKey, counts []int64) error

type ObjectUsage struct {
	mu     sync.Mutex
	counts map[usageKey]int64
	limit  int
	post   usagePost
	report func(error)
	full   chan struct{}
	stop   chan struct{}
	done   chan struct{}
	once   sync.Once
}

func startObjectUsage(post usagePost, report func(error), interval time.Duration, limit int) *ObjectUsage {
	u := &ObjectUsage{
		counts: make(map[usageKey]int64),
		limit:  limit,
		post:   post,
		report: report,
		full:   make(chan struct{}, 1),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go u.run(interval)
	return u
}

func (u *ObjectUsage) Add(userID uuid.UUID, objectType string, objectID uuid.UUID) {
	if u == nil || userID == uuid.Nil || objectID == uuid.Nil || objectType == "" {
		return
	}
	u.mu.Lock()
	u.counts[usageKey{UserID: userID, ObjectType: objectType, ObjectID: objectID}]++
	full := len(u.counts) >= u.limit
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
	counts := make([]int64, 0, len(counted))
	for k, n := range counted {
		keys = append(keys, k)
		counts = append(counts, n)
	}
	ctx, cancel := context.WithTimeout(context.Background(), usageFlushTimeout)
	defer cancel()
	if err := u.post(ctx, keys, counts); err != nil && u.report != nil {
		u.report(err)
	}
}

func (d *DB) postObjectUsage(ctx context.Context, keys []usageKey, counts []int64) error {
	userIDs := make([]uuid.UUID, len(keys))
	objectTypes := make([]string, len(keys))
	objectIDs := make([]uuid.UUID, len(keys))
	for i, k := range keys {
		userIDs[i] = k.UserID
		objectTypes[i] = k.ObjectType
		objectIDs[i] = k.ObjectID
	}
	_, err := d.NewRaw("SELECT claimius.touch_object_usage(?::uuid[], ?::text[], ?::uuid[], ?::bigint[])",
		pq.Array(userIDs), pq.Array(objectTypes), pq.Array(objectIDs), pq.Array(counts)).Exec(ctx)
	return err
}
