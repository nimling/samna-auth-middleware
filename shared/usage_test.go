package shared

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type usageSink struct {
	mu      sync.Mutex
	flushes []map[usageKey]int64
	err     error
}

func (s *usageSink) post(_ context.Context, keys []usageKey, counts []int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := make(map[usageKey]int64, len(keys))
	for i, k := range keys {
		m[k] = counts[i]
	}
	s.flushes = append(s.flushes, m)
	return s.err
}

func (s *usageSink) seen() []map[usageKey]int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[usageKey]int64(nil), s.flushes...)
}

func TestObjectUsage_CountsPerKeyAndFlushesOnStop(t *testing.T) {
	sink := &usageSink{}
	u := startObjectUsage(sink.post, nil, time.Hour, 1000)
	user, object := uuid.New(), uuid.New()
	for i := 0; i < 3; i++ {
		u.Add(user, OBJECT_TYPE_ORGANIZATION, object)
	}
	u.Add(user, OBJECT_TYPE_LOCATION, object)
	u.Add(uuid.Nil, OBJECT_TYPE_LOCATION, object)
	u.Stop()
	flushes := sink.seen()
	if len(flushes) != 1 {
		t.Fatalf("expected one flush on stop, got %d", len(flushes))
	}
	if got := flushes[0][usageKey{UserID: user, ObjectType: OBJECT_TYPE_ORGANIZATION, ObjectID: object}]; got != 3 {
		t.Fatalf("expected 3 uses of the organization, got %d", got)
	}
	if len(flushes[0]) != 2 {
		t.Fatalf("expected two keys, the nil user dropped, got %v", flushes[0])
	}
}

func TestObjectUsage_FlushesAtTheKeyLimit(t *testing.T) {
	sink := &usageSink{}
	u := startObjectUsage(sink.post, nil, time.Hour, 3)
	defer u.Stop()
	user := uuid.New()
	for i := 0; i < 3; i++ {
		u.Add(user, OBJECT_TYPE_ORGANIZATION, uuid.New())
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(sink.seen()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	flushes := sink.seen()
	if len(flushes) != 1 || len(flushes[0]) != 3 {
		t.Fatalf("expected one flush of three keys at the limit, got %v", flushes)
	}
}

func TestObjectUsage_FlushesOnTheInterval(t *testing.T) {
	sink := &usageSink{err: errors.New("down")}
	var reported error
	var mu sync.Mutex
	u := startObjectUsage(sink.post, func(err error) { mu.Lock(); reported = err; mu.Unlock() }, 20*time.Millisecond, 1000)
	defer u.Stop()
	u.Add(uuid.New(), OBJECT_TYPE_ORGANIZATION, uuid.New())
	deadline := time.Now().Add(2 * time.Second)
	for len(sink.seen()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(sink.seen()) == 0 {
		t.Fatal("expected a flush on the interval")
	}
	failed := func() bool { mu.Lock(); defer mu.Unlock(); return reported != nil }
	for !failed() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !failed() {
		t.Fatal("a failed flush must be reported")
	}
}

func TestObjectUsage_NilIsANoOp(t *testing.T) {
	var u *ObjectUsage
	u.Add(uuid.New(), OBJECT_TYPE_ORGANIZATION, uuid.New())
	u.Stop()
}
