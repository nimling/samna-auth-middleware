//go:build dbtest

package sam_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	sam "github.com/nimling/samna-auth-middleware/v2/disciple/sam"
	sdb "github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
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
		`INSERT INTO claimius.disciple_state (last_applied_txid, last_applied_seq) SELECT 0, 0 WHERE NOT EXISTS (SELECT 1 FROM claimius.disciple_state)`); err != nil {
		t.Fatalf("cursor row: %v", err)
	}
	return db
}

func organizationPayload(at client.SyncCursor, id uuid.UUID) sam.SyncPayload {
	return sam.SyncPayload{
		SyncCursor: at,
		Operation:  "upsert",
		EventType:  "claimius.organization",
		Payload: json.RawMessage(fmt.Sprintf(
			`{"id":%q,"name":"Page","type":"company","private_seed":"seed","sa_owner_id":%q,"sa_root_id":%q,"sa_level":0,"sa_created_by":%q,"sa_created_at":"2026-01-01T00:00:00Z","sa_updated_at":"2026-01-01T00:00:00Z"}`,
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

func step(c client.SyncCursor, n int64) client.SyncCursor {
	return client.SyncCursor{Txid: c.Txid, Seq: c.Seq + n}
}

func cursor(t *testing.T) client.SyncCursor {
	t.Helper()
	seq, err := sdb.Instance().GetSeq(context.Background())
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
		organizationPayload(step(from, 1), lost),
		{SyncCursor: step(from, 2), Operation: "upsert", EventType: "claimius.unknown", Payload: json.RawMessage(`{}`)},
	}
	if err := sam.Disciple().ApplyPage(ctx, from, sam.State{Cursor: step(from, 2)}, failing, ""); err == nil {
		t.Fatal("a page carrying an event the replica cannot apply must fail")
	}
	if organizationExists(t, db, lost) {
		t.Fatal("an event applied before the failing one in the same page must roll back with it")
	}
	if got := cursor(t); got != from {
		t.Fatalf("a failed page must leave the cursor at %s, got %s", from, got)
	}

	if err := sam.Disciple().ApplyPage(ctx, from, sam.State{Cursor: step(from, 1)}, []sam.SyncPayload{organizationPayload(step(from, 1), kept)}, ""); err != nil {
		t.Fatalf("apply page: %v", err)
	}
	if !organizationExists(t, db, kept) {
		t.Fatal("an applied page must commit its events")
	}
	if got := cursor(t); got != step(from, 1) {
		t.Fatalf("an applied page must move the cursor to %s, got %s", step(from, 1), got)
	}

	if err := sam.Disciple().ApplyPage(ctx, from, sam.State{Cursor: step(from, 1)}, []sam.SyncPayload{organizationPayload(step(from, 1), lost)}, ""); err == nil {
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
				seq, err := sdb.Instance().GetSeq(ctx)
				if err == nil && sam.Disciple().ApplyPage(ctx, seq, sam.State{Cursor: step(seq, 1)}, nil, "") == nil {
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
	go func() { defer close(doneA); shared.LeadSync(ctxA, db, key, interval, loop("a")) }()
	go func() { defer close(doneB); shared.LeadSync(ctxB, db, key, interval, loop("b")) }()

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

func TestWaitForSeqReturnsOnceTheCursorReachesTheWrite(t *testing.T) {
	database(t)
	ctx := context.Background()
	from := cursor(t)

	go func() {
		time.Sleep(200 * time.Millisecond)
		if err := sam.Disciple().ApplyPage(ctx, from, sam.State{Cursor: step(from, 3)}, nil, ""); err != nil {
			t.Errorf("apply page: %v", err)
		}
	}()
	if err := sdb.Instance().WaitForSeq(ctx, step(from, 3), 5*time.Second); err != nil {
		t.Fatalf("a wait for a sequence the cursor reaches must return, got %v", err)
	}
	if err := sdb.Instance().WaitForSeq(ctx, step(from, 1000), 200*time.Millisecond); err == nil {
		t.Fatal("a wait for a sequence the cursor never reaches must time out")
	}
	later := client.SyncCursor{Txid: from.Txid + 1, Seq: 1}
	if err := sdb.Instance().WaitForSeq(ctx, later, 200*time.Millisecond); err == nil {
		t.Fatal("a write of a later transaction is waited on even when its sequence is below the cursor")
	}
}

func TestApplyPageSkipsUnchangedRows(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	id := uuid.New()
	version := func() string {
		t.Helper()
		var xmin string
		if err := db.NewRaw(`SELECT xmin::TEXT FROM claimius.organization WHERE id = ?`, id).Scan(ctx, &xmin); err != nil {
			t.Fatalf("read row version: %v", err)
		}
		return xmin
	}

	from := cursor(t)
	if err := sam.Disciple().ApplyPage(ctx, from, sam.State{Cursor: step(from, 1)}, []sam.SyncPayload{organizationPayload(step(from, 1), id)}, ""); err != nil {
		t.Fatalf("apply page: %v", err)
	}
	written := version()

	if err := sam.Disciple().ApplyPage(ctx, step(from, 1), sam.State{Cursor: step(from, 2)}, []sam.SyncPayload{organizationPayload(step(from, 2), id)}, ""); err != nil {
		t.Fatalf("apply the same row again: %v", err)
	}
	if got := version(); got != written {
		t.Fatalf("a row the disciple already holds unchanged must not be rewritten, version %s became %s", written, got)
	}

	renamed := organizationPayload(step(from, 3), id)
	renamed.Payload = json.RawMessage(strings.Replace(string(renamed.Payload), `"name":"Page"`, `"name":"Renamed"`, 1))
	if err := sam.Disciple().ApplyPage(ctx, step(from, 2), sam.State{Cursor: step(from, 3)}, []sam.SyncPayload{renamed}, ""); err != nil {
		t.Fatalf("apply a changed row: %v", err)
	}
	if got := version(); got == written {
		t.Fatal("a changed row must be written")
	}
}

func getFakeProphet(name string, cursor client.SyncCursor, org uuid.UUID, serves func(bound string) bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bound := r.URL.Query().Get("prophet")
		if !serves(bound) {
			w.Header().Set(client.HeaderHome, bound)
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.Header().Set(client.HeaderProphet, name)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/sync/snapshot") {
			snapshot, _ := json.Marshal(sam.SnapshotResponse{Records: []sam.SyncPayload{organizationPayload(cursor, org)}, SyncCursor: cursor})
			fmt.Fprintf(w, `{"data":%s,"count":1}`, snapshot)
			return
		}
		w.Write([]byte(`{"data":[],"count":0}`))
	}))
}

func TestDiscipleMovesOnlyToAProphetThatTookItsAppOver(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `UPDATE claimius.disciple_state SET prophet = NULL, endpoint = NULL`); err != nil {
		t.Fatalf("unbind: %v", err)
	}
	disciple := sam.Disciple()
	start := cursor(t)
	oldOrg, nextOrg := uuid.New(), uuid.New()
	var agreed atomic.Bool

	old := getFakeProphet("gotest_old", client.SyncCursor{Txid: start.Txid + 10}, oldOrg, func(string) bool { return true })
	defer old.Close()
	next := getFakeProphet("gotest_next", client.SyncCursor{Txid: 7}, nextOrg, func(bound string) bool {
		return bound == "gotest_old" && agreed.Load()
	})
	defer next.Close()

	t.Setenv("SAUTH_ENDPOINT", old.URL)
	if err := disciple.PullSnapshot(ctx, "", ""); err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	bound, err := disciple.GetState(ctx)
	if err != nil || bound.Prophet != "gotest_old" || bound.Endpoint != old.URL {
		t.Fatalf("the first answer must bind the disciple to the prophet that gave it, got %+v %v", bound, err)
	}

	t.Setenv("SAUTH_ENDPOINT", next.URL)
	if err := disciple.Move(ctx); !errors.Is(err, client.ErrNotHome) {
		t.Fatalf("a prophet without an agreed handover must refuse the disciple, got %v", err)
	}
	if kept, _ := disciple.GetState(ctx); kept != bound || organizationExists(t, db, nextOrg) {
		t.Fatalf("a refused move must keep the prophet, the cursor and the replica, got %+v", kept)
	}
	if _, err := disciple.PullSync(ctx); err != nil {
		t.Fatalf("a refused disciple must keep pulling from its prophet, got %v", err)
	}

	agreed.Store(true)
	if err := disciple.Move(ctx); err != nil {
		t.Fatalf("a prophet that took the app over must serve the disciple, got %v", err)
	}
	moved, err := disciple.GetState(ctx)
	if err != nil || moved.Prophet != "gotest_next" || moved.Endpoint != next.URL || moved.Cursor != (client.SyncCursor{Txid: 7}) {
		t.Fatalf("an agreed move must bind the disciple to the new prophet at its snapshot's position, got %+v %v", moved, err)
	}
	if !organizationExists(t, db, nextOrg) {
		t.Fatal("an agreed move must apply the new prophet's snapshot")
	}
}

func TestApplyPageStoresTheUserProfileFields(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	from := cursor(t)
	id := uuid.New()
	userID := uuid.New()

	payload := sam.SyncPayload{
		SyncCursor: step(from, 1),
		Operation:  "upsert",
		EventType:  "claimius.samna_user",
		Payload: json.RawMessage(fmt.Sprintf(
			`{"id":%q,"user_id":%q,"first_name":"Profile","email":"profile@example.no","bio":"Builds access control","job_title":"Engineer","company":"Nimtech","website":"https://example.no","linkedin":"https://www.linkedin.com/in/example","status":"active","sa_created_at":"2026-01-01T00:00:00Z","sa_updated_at":"2026-01-01T00:00:00Z"}`,
			id, userID)),
	}
	if err := sam.Disciple().ApplyPage(ctx, from, sam.State{Cursor: step(from, 1)}, []sam.SyncPayload{payload}, ""); err != nil {
		t.Fatalf("apply page: %v", err)
	}

	var stored shared.SamnaUser
	if err := db.NewSelect().Model(&stored).Where("id = ?", id).Scan(ctx); err != nil {
		t.Fatalf("read user: %v", err)
	}
	got := []*string{stored.Bio, stored.JobTitle, stored.Company, stored.Website, stored.Linkedin}
	want := []string{"Builds access control", "Engineer", "Nimtech", "https://example.no", "https://www.linkedin.com/in/example"}
	for i, value := range got {
		if value == nil || *value != want[i] {
			t.Fatalf("profile field %d: want %q, got %v", i, want[i], value)
		}
	}
}

func TestPullBacksOffWhileTheSnapshotFailsToApply(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	var snapshots atomic.Int32
	prophet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sync/snapshot") {
			snapshots.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusConflict)
	}))
	defer prophet.Close()
	t.Setenv("SAUTH_ENDPOINT", prophet.URL)
	if _, err := db.ExecContext(ctx, `UPDATE claimius.disciple_state SET prophet = NULL, endpoint = ?, last_applied_seq = greatest(last_applied_seq, 1)`, prophet.URL); err != nil {
		t.Fatalf("bind: %v", err)
	}

	run, stop := context.WithTimeout(ctx, 3500*time.Millisecond)
	defer stop()
	sam.Disciple().Pull(run)

	if n := snapshots.Load(); n == 0 || n > 3 {
		t.Fatalf("a snapshot that fails to apply must be pulled again only after the backoff, 1 s then 2 s, got %d snapshots in 3.5 s", n)
	}
}

func TestApplyPageAppliesAUserToken(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	from := cursor(t)
	orgID, tokenID, userID := uuid.New(), uuid.New(), uuid.New()
	external := "ext-" + uuid.NewString()
	page := []sam.SyncPayload{
		organizationPayload(step(from, 1), orgID),
		{SyncCursor: step(from, 2), Operation: "upsert", EventType: "claimius.user_token", Payload: json.RawMessage(fmt.Sprintf(
			`{"id":%q,"app_id":%q,"user_id":%q,"id_claim":"sub","external_id":%q,"type":"external","sa_owner_id":%q,"sa_created_at":"2026-01-01T00:00:00Z","sa_updated_at":"2026-01-01T00:00:00Z"}`,
			tokenID, uuid.New(), userID, external, orgID))},
	}
	if err := sam.Disciple().ApplyPage(ctx, from, sam.State{Cursor: step(from, 2)}, page, ""); err != nil {
		t.Fatalf("apply token: %v", err)
	}
	tokens, err := db.GetUserTokens(ctx, []uuid.UUID{userID})
	if err != nil || len(tokens) != 1 || tokens[0].ExternalID != external || tokens[0].OwnerID != orgID {
		t.Fatalf("an applied token must be read back by user, got %v %v", tokens, err)
	}
	deletion := []sam.SyncPayload{{SyncCursor: step(from, 3), Operation: "delete", EventType: "claimius.user_token", Payload: json.RawMessage(fmt.Sprintf(`{"id":%q}`, tokenID))}}
	if err := sam.Disciple().ApplyPage(ctx, step(from, 2), sam.State{Cursor: step(from, 3)}, deletion, ""); err != nil {
		t.Fatalf("delete token: %v", err)
	}
	if tokens, err := db.GetUserTokens(ctx, []uuid.UUID{userID}); err != nil || len(tokens) != 0 {
		t.Fatalf("a deleted token must be gone, got %v %v", tokens, err)
	}
}

func TestApplyPageAppliesAnAuditRow(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	from := cursor(t)
	orgID, auditID := uuid.New(), uuid.New()
	page := []sam.SyncPayload{
		organizationPayload(step(from, 1), orgID),
		{SyncCursor: step(from, 2), Operation: "upsert", EventType: "claimius.audit", Payload: json.RawMessage(fmt.Sprintf(
			`{"id":%q,"app_id":%q,"operation":"create","message":"","type":"claim","object_type":"claimius.claim","object_id":%q,"sa_owner_id":%q,"sa_created_at":"2026-01-01T00:00:00Z","sa_updated_at":"2026-01-01T00:00:00Z"}`,
			auditID, uuid.New(), uuid.New(), orgID))},
	}
	if err := sam.Disciple().ApplyPage(ctx, from, sam.State{Cursor: step(from, 2)}, page, ""); err != nil {
		t.Fatalf("apply audit: %v", err)
	}
	var stored int
	if err := db.NewRaw(`SELECT count(*) FROM claimius.audit WHERE id = ?`, auditID).Scan(ctx, &stored); err != nil || stored != 1 {
		t.Fatalf("an applied audit row must be stored once, got %d %v", stored, err)
	}
	if err := sam.Disciple().ApplyPage(ctx, step(from, 2), sam.State{Cursor: step(from, 3)}, []sam.SyncPayload{{SyncCursor: step(from, 3), Operation: page[1].Operation, EventType: page[1].EventType, Payload: page[1].Payload}}, ""); err != nil {
		t.Fatalf("a repeated audit row must apply without error: %v", err)
	}
	if err := db.NewRaw(`SELECT count(*) FROM claimius.audit WHERE id = ?`, auditID).Scan(ctx, &stored); err != nil || stored != 1 {
		t.Fatalf("a repeated audit row must not duplicate, got %d %v", stored, err)
	}
}
