//go:build dbtest

package sam_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	sam "github.com/nimling/samna-auth-middleware/disciple/sam"
	sdb "github.com/nimling/samna-auth-middleware/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/shared"
)

func database(t *testing.T) *shared.DB {
	t.Helper()
	if sdb.Instance() == nil {
		if _, err := sdb.Init(echo.New().Logger); err != nil {
			t.Fatalf("connect: %v", err)
		}
	}
	db := sdb.Instance().DB
	if _, err := db.ExecContext(context.Background(),
		`INSERT INTO claimius.disciple_state (last_applied_seq) SELECT 0 WHERE NOT EXISTS (SELECT 1 FROM claimius.disciple_state)`); err != nil {
		t.Fatalf("cursor row: %v", err)
	}
	return db
}

func organizationPayload(seq int64, id uuid.UUID) sam.SyncPayload {
	return sam.SyncPayload{
		Seq:       seq,
		Operation: "upsert",
		EventType: "claimius.organization",
		Payload: json.RawMessage(fmt.Sprintf(
			`{"id":%q,"name":"Page","type":"company","private_seed":"seed","sa_owner_id":%q,"sa_root_id":%q,"sa_level":0,"sa_created_by":%q}`,
			id, id, id, id)),
	}
}

func organizationExists(t *testing.T, db *shared.DB, id uuid.UUID) bool {
	t.Helper()
	var exists bool
	if err := db.NewRaw(`SELECT EXISTS (SELECT 1 FROM claimius.organization WHERE id = ?)`, id).Scan(context.Background(), &exists); err != nil {
		t.Fatalf("probe organization: %v", err)
	}
	return exists
}

func cursor(t *testing.T) int64 {
	t.Helper()
	seq, err := sam.GetSeq(context.Background())
	if err != nil {
		t.Fatalf("read cursor: %v", err)
	}
	return seq
}

func TestApplyPageCommitsOrRollsBackWhole(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	from := cursor(t)
	kept := uuid.New()
	lost := uuid.New()

	failing := []sam.SyncPayload{
		organizationPayload(from+1, lost),
		{Seq: from + 2, Operation: "upsert", EventType: "claimius.unknown", Payload: json.RawMessage(`{}`)},
	}
	if err := sam.ApplyPage(ctx, from, from+2, failing); err == nil {
		t.Fatal("a page carrying an event the replica cannot apply must fail")
	}
	if organizationExists(t, db, lost) {
		t.Fatal("an event applied before the failing one in the same page must roll back with it")
	}
	if got := cursor(t); got != from {
		t.Fatalf("a failed page must leave the cursor at %d, got %d", from, got)
	}

	if err := sam.ApplyPage(ctx, from, from+1, []sam.SyncPayload{organizationPayload(from+1, kept)}); err != nil {
		t.Fatalf("apply page: %v", err)
	}
	if !organizationExists(t, db, kept) {
		t.Fatal("an applied page must commit its events")
	}
	if got := cursor(t); got != from+1 {
		t.Fatalf("an applied page must move the cursor to %d, got %d", from+1, got)
	}

	if err := sam.ApplyPage(ctx, from, from+1, []sam.SyncPayload{organizationPayload(from+1, lost)}); err == nil {
		t.Fatal("a page pulled at a cursor another consumer already moved must be refused")
	}
	if organizationExists(t, db, lost) {
		t.Fatal("a refused page must apply nothing")
	}
}

func TestLeadSyncRunsOneConsumer(t *testing.T) {
	db := database(t)
	key := sam.SyncLockKey("dbtest-" + uuid.NewString())
	interval := 100 * time.Millisecond

	var mu sync.Mutex
	applied := map[string]int{}
	loop := func(name string) func(context.Context) {
		return func(ctx context.Context) {
			for ctx.Err() == nil {
				seq, err := sam.GetSeq(ctx)
				if err == nil && sam.ApplyPage(ctx, seq, seq+1, nil) == nil {
					mu.Lock()
					applied[name]++
					mu.Unlock()
				}
				select {
				case <-ctx.Done():
				case <-time.After(10 * time.Millisecond):
				}
			}
		}
	}
	counts := func() (int, int) {
		mu.Lock()
		defer mu.Unlock()
		return applied["a"], applied["b"]
	}

	ctxA, stopA := context.WithCancel(context.Background())
	ctxB, stopB := context.WithCancel(context.Background())
	defer stopA()
	defer stopB()
	doneA := make(chan struct{})
	doneB := make(chan struct{})
	go func() { defer close(doneA); sam.LeadSync(ctxA, db, key, interval, loop("a")) }()
	go func() { defer close(doneB); sam.LeadSync(ctxB, db, key, interval, loop("b")) }()

	time.Sleep(10 * interval)
	a, b := counts()
	if (a > 0) == (b > 0) {
		t.Fatalf("exactly one of two loops against one database must apply, got a=%d b=%d", a, b)
	}

	leader, follower := "a", "b"
	stopLeader, leaderDone := stopA, doneA
	if b > 0 {
		leader, follower = "b", "a"
		stopLeader, leaderDone = stopB, doneB
	}
	stopLeader()
	<-leaderDone
	mu.Lock()
	before := applied[leader]
	mu.Unlock()

	time.Sleep(10 * interval)
	mu.Lock()
	defer mu.Unlock()
	if applied[follower] == 0 {
		t.Fatalf("the waiting loop must take over once the holder stops, %s applied nothing", follower)
	}
	if applied[leader] != before {
		t.Fatalf("a stopped holder must apply nothing more, %s went from %d to %d", leader, before, applied[leader])
	}
}
