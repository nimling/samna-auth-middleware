//go:build dbtest

package echo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	pecho "github.com/nimling/samna-auth-middleware/prophet/echo"
	psdb "github.com/nimling/samna-auth-middleware/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/smw"
)

func getAppHead(t *testing.T, appID uuid.UUID) int64 {
	t.Helper()
	var seq int64
	if err := psdb.Instance().NewRaw(`SELECT claimius.get_sync_seq(?)`, appID).Scan(context.Background(), &seq); err != nil {
		t.Fatalf("read app head: %v", err)
	}
	return seq
}

func postSyncEvent(t *testing.T, appID uuid.UUID) {
	t.Helper()
	if _, err := psdb.Instance().ExecContext(context.Background(), `
		INSERT INTO claimius.sync_event (operation, event_type, payload, app_id)
		VALUES ('upsert', 'claimius.user_field', jsonb_build_object('id', gen_random_uuid()), ?)
	`, appID); err != nil {
		t.Errorf("write event: %v", err)
	}
}

func getSyncPage(appID uuid.UUID, seq int64, query string) *httptest.ResponseRecorder {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/?"+query, nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("seq")
	ctx.SetParamValues(strconv.FormatInt(seq, 10))
	ctx.Set("app_id", appID)
	ctx.Set("request_id", uuid.New())
	_ = pecho.GetSyncEvents(ctx)
	return rec
}

func getPageSeqs(t *testing.T, rec *httptest.ResponseRecorder) []int64 {
	t.Helper()
	var envelope struct {
		Data []pecho.SyncPayload `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode page %s: %v", rec.Body.String(), err)
	}
	seqs := make([]int64, 0, len(envelope.Data))
	for _, p := range envelope.Data {
		seqs = append(seqs, p.Seq)
	}
	return seqs
}

func TestSyncSeqAnswersAWriteWithTheAppsSequence(t *testing.T) {
	appID, claimID := getGrantFixture(t)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"reason":"sequenced"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("claim_id", "id")
	ctx.SetParamValues(claimID.String(), grantUser.String())
	ctx.Set("app_id", appID)
	ctx.Set("request_id", uuid.New())
	if err := smw.SyncSeq(pecho.PatchClaimUser)(ctx); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("patch grant: %v %d %s", err, rec.Code, rec.Body.String())
	}

	got, err := strconv.ParseInt(rec.Header().Get(client.HeaderSyncSeq), 10, 64)
	if err != nil {
		t.Fatalf("a write must answer with %s, got %q", client.HeaderSyncSeq, rec.Header().Get(client.HeaderSyncSeq))
	}
	var own int64
	if err := psdb.Instance().NewRaw(`
		SELECT max(seq) FROM claimius.sync_event
		WHERE app_id = ? AND event_type = 'claimius.user_claim' AND payload ->> 'reason' = 'sequenced'
	`, appID).Scan(context.Background(), &own); err != nil {
		t.Fatalf("read the write's event: %v", err)
	}
	if got < own || got != getAppHead(t, appID) {
		t.Fatalf("the answer must carry the app's sequence covering the write's event %d, got %d", own, got)
	}

	read := httptest.NewRecorder()
	readCtx := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), read)
	readCtx.Set("app_id", appID)
	if err := smw.SyncSeq(func(c echo.Context) error { return c.NoContent(http.StatusOK) })(readCtx); err != nil {
		t.Fatalf("read: %v", err)
	}
	if read.Header().Get(client.HeaderSyncSeq) != "" {
		t.Fatal("a read carries no sequence")
	}
}

func TestSyncEventsHoldsAnEmptyPullUntilAnEventCommits(t *testing.T) {
	appID, _ := getGrantFixture(t)
	head := getAppHead(t, appID)

	go func() {
		time.Sleep(500 * time.Millisecond)
		postSyncEvent(t, appID)
	}()
	start := time.Now()
	rec := getSyncPage(appID, head, "wait=10&consumer="+uuid.NewString())
	took := time.Since(start)
	if rec.Code != http.StatusOK {
		t.Fatalf("held pull: %d %s", rec.Code, rec.Body.String())
	}
	if seqs := getPageSeqs(t, rec); len(seqs) != 1 || seqs[0] <= head {
		t.Fatalf("the held pull must answer with the event that committed, got %v", seqs)
	}
	if took < 400*time.Millisecond || took > 5*time.Second {
		t.Fatalf("the held pull must answer when the event commits, took %s", took)
	}

	start = time.Now()
	rec = getSyncPage(appID, getAppHead(t, appID), "wait=1")
	if seqs := getPageSeqs(t, rec); len(seqs) != 0 || time.Since(start) < 900*time.Millisecond {
		t.Fatalf("a pull with nothing to serve must answer empty after the wait, got %v after %s", seqs, time.Since(start))
	}

	start = time.Now()
	rec = getSyncPage(appID, getAppHead(t, appID), "")
	if seqs := getPageSeqs(t, rec); len(seqs) != 0 || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("a pull without wait must answer at once, got %v after %s", seqs, time.Since(start))
	}
}

func TestSyncEventsKeepsACursorPerConsumer(t *testing.T) {
	appID, _ := getGrantFixture(t)
	head := getAppHead(t, appID)
	first, second := uuid.New(), uuid.New()

	getSyncPage(appID, head, "consumer="+first.String())
	getSyncPage(appID, 0, "consumer="+second.String())

	var cursors []int64
	if err := psdb.Instance().NewRaw(`
		SELECT last_applied_seq FROM claimius.sync_consumer
		WHERE app_id = ? AND consumer_id IN (?, ?)
		ORDER BY last_applied_seq DESC
	`, appID, first, second).Scan(context.Background(), &cursors); err != nil {
		t.Fatalf("read cursors: %v", err)
	}
	if len(cursors) != 2 || cursors[0] != head || cursors[1] != 0 {
		t.Fatalf("two consumers of one app must keep their own cursors, got %v", cursors)
	}
}

func TestSyncEventsSendsAStaleCursorToTheSnapshot(t *testing.T) {
	appID, _ := getGrantFixture(t)
	var issued int64
	if err := psdb.Instance().NewRaw(`SELECT pg_sequence_last_value('claimius.sync_event_seq_seq')`).Scan(context.Background(), &issued); err != nil {
		t.Fatalf("read sequence: %v", err)
	}

	if rec := getSyncPage(appID, issued+1000, "wait=10"); rec.Code != http.StatusConflict {
		t.Fatalf("a cursor above every issued sequence must answer 409, got %d %s", rec.Code, rec.Body.String())
	}

	var rows int64
	if err := psdb.Instance().NewRaw(`SELECT claimius.count_sync_source_rows(?)`, appID).Scan(context.Background(), &rows); err != nil {
		t.Fatalf("count source rows: %v", err)
	}
	head := getAppHead(t, appID)
	if _, err := psdb.Instance().ExecContext(context.Background(), `
		INSERT INTO claimius.sync_event (operation, event_type, payload, app_id)
		SELECT 'upsert', 'claimius.user_field', jsonb_build_object('id', gen_random_uuid()), ?
		FROM generate_series(1, ?)
	`, appID, rows+1); err != nil {
		t.Fatalf("write backlog: %v", err)
	}
	if rec := getSyncPage(appID, head, ""); rec.Code != http.StatusConflict {
		t.Fatalf("a cursor behind by more events than the app has source rows must answer 409, got %d", rec.Code)
	}
	if rec := getSyncPage(appID, getAppHead(t, appID), ""); rec.Code != http.StatusOK {
		t.Fatalf("a cursor at the head must be served, got %d", rec.Code)
	}
}
