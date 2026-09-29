package shared

import (
	"context"
	"errors"
	"fmt"
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
		u.Add(nil, user, OBJECT_TYPE_ORGANIZATION, object)
	}
	u.Add(nil, user, OBJECT_TYPE_LOCATION, object)
	u.Add(nil, uuid.Nil, OBJECT_TYPE_LOCATION, object)
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
		u.Add(nil, user, OBJECT_TYPE_ORGANIZATION, uuid.New())
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
	u.Add(nil, uuid.New(), OBJECT_TYPE_ORGANIZATION, uuid.New())
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
	u.Add(nil, uuid.New(), OBJECT_TYPE_ORGANIZATION, uuid.New())
	u.Stop()
}

type pgCodeError string

func (e pgCodeError) Error() string { return "sqlstate " + string(e) }

func (e pgCodeError) Field(f byte) string {
	if f == 'C' {
		return string(e)
	}
	return ""
}

type orderedSink struct {
	mu     sync.Mutex
	posts  [][]usageKey
	counts [][]int64
	fail   []error
}

func (s *orderedSink) post(_ context.Context, keys []usageKey, counts []int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.posts = append(s.posts, append([]usageKey(nil), keys...))
	s.counts = append(s.counts, append([]int64(nil), counts...))
	if len(s.fail) > 0 {
		err := s.fail[0]
		s.fail = s.fail[1:]
		return err
	}
	return nil
}

func (s *orderedSink) seen() ([][]usageKey, [][]int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]usageKey(nil), s.posts...), append([][]int64(nil), s.counts...)
}

func TestObjectUsage_KeysOnTheAppChecked(t *testing.T) {
	sink := &usageSink{}
	u := startObjectUsage(sink.post, nil, time.Hour, 1000)
	user, object := uuid.New(), uuid.New()
	appA, appB := uuid.New(), uuid.New()
	u.Add(&appA, user, OBJECT_TYPE_ORGANIZATION, object)
	u.Add(&appB, user, OBJECT_TYPE_ORGANIZATION, object)
	u.Add(nil, user, OBJECT_TYPE_ORGANIZATION, object)
	u.Add(nil, user, OBJECT_TYPE_ORGANIZATION, object)
	u.Stop()
	flushes := sink.seen()
	if len(flushes) != 1 || len(flushes[0]) != 3 {
		t.Fatalf("expected one flush of three keys, one per app, got %v", flushes)
	}
	for app, want := range map[uuid.UUID]int64{appA: 1, appB: 1, uuid.Nil: 2} {
		if got := flushes[0][usageKey{AppID: app, UserID: user, ObjectType: OBJECT_TYPE_ORGANIZATION, ObjectID: object}]; got != want {
			t.Fatalf("app %s: expected %d uses, got %d", app, want, got)
		}
	}
}

func TestObjectUsage_FlushesInTheLockOrder(t *testing.T) {
	sink := &orderedSink{}
	u := startObjectUsage(sink.post, nil, time.Hour, 1000)
	for i := 0; i < 50; i++ {
		app := uuid.New()
		u.Add(&app, uuid.New(), OBJECT_TYPE_ORGANIZATION, uuid.New())
		u.Add(nil, uuid.New(), OBJECT_TYPE_LOCATION, uuid.New())
	}
	u.Stop()
	posts, _ := sink.seen()
	if len(posts) != 1 || len(posts[0]) != 100 {
		t.Fatalf("expected one flush of 100 keys, got %d flushes", len(posts))
	}
	for i := 1; i < len(posts[0]); i++ {
		if posts[0][i].less(posts[0][i-1]) {
			t.Fatalf("key %d is out of the user, app, type, object order", i)
		}
	}
}

func TestObjectUsage_RetriesADeadlock(t *testing.T) {
	for _, code := range []string{"40P01", "40001"} {
		sink := &orderedSink{fail: []error{fmt.Errorf("flush: %w", pgCodeError(code))}}
		var reported error
		u := startObjectUsage(sink.post, func(err error) { reported = err }, time.Hour, 1000)
		user, object := uuid.New(), uuid.New()
		u.Add(nil, user, OBJECT_TYPE_ORGANIZATION, object)
		u.Add(nil, user, OBJECT_TYPE_ORGANIZATION, object)
		u.Stop()
		posts, counts := sink.seen()
		if len(posts) != 2 || counts[1][0] != 2 {
			t.Fatalf("%s: expected the batch posted again with its counts, got %v %v", code, posts, counts)
		}
		if reported != nil {
			t.Fatalf("%s: a retried batch that lands must not be reported, got %v", code, reported)
		}
	}
}

func TestObjectUsage_KeepsTheCountsOfAFailedFlush(t *testing.T) {
	sink := &orderedSink{fail: []error{errors.New("down")}}
	var mu sync.Mutex
	var reported error
	u := startObjectUsage(sink.post, func(err error) { mu.Lock(); reported = err; mu.Unlock() }, 20*time.Millisecond, 1000)
	user, object := uuid.New(), uuid.New()
	u.Add(nil, user, OBJECT_TYPE_ORGANIZATION, object)
	u.Add(nil, user, OBJECT_TYPE_ORGANIZATION, object)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if posts, _ := sink.seen(); len(posts) >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	u.Stop()
	posts, counts := sink.seen()
	if len(posts) < 2 {
		t.Fatalf("expected the failed batch flushed again, got %d flushes", len(posts))
	}
	if counts[0][0] != 2 || counts[1][0] != 2 {
		t.Fatalf("a failed flush must keep its counts for the next one, got %v", counts)
	}
	mu.Lock()
	defer mu.Unlock()
	if reported == nil {
		t.Fatal("a failed flush must be reported")
	}
}
