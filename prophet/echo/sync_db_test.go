//go:build dbtest

package echo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	pecho "github.com/nimling/samna-auth-middleware/v2/prophet/echo"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/smw"
)

func getAppHead(t *testing.T, appID uuid.UUID) client.SyncCursor {
	t.Helper()
	var head client.SyncCursor
	if err := psdb.Instance().NewRaw(`SELECT txid, seq FROM claimius.get_sync_seq(?)`, appID).Scan(context.Background(), &head); err != nil {
		t.Fatalf("read app head: %v", err)
	}
	return head
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

func getSyncPage(appID uuid.UUID, seq client.SyncCursor, query string) *httptest.ResponseRecorder {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/?"+query, nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("cursor")
	ctx.SetParamValues(seq.String())
	ctx.Set("app_id", appID)
	ctx.Set("request_id", uuid.New())
	_ = pecho.GetSyncEvents(ctx)
	return rec
}

func getPageSeqs(t *testing.T, rec *httptest.ResponseRecorder) []client.SyncCursor {
	t.Helper()
	var envelope struct {
		Data []pecho.SyncPayload `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode page %s: %v", rec.Body.String(), err)
	}
	seqs := make([]client.SyncCursor, 0, len(envelope.Data))
	for _, p := range envelope.Data {
		seqs = append(seqs, p.SyncCursor)
	}
	return seqs
}

func TestSyncSeqAnswersAWriteWithTheAppsSequence(t *testing.T) {
	appID, claimID := getGrantFixture(t)
	grantID, _ := getGrantRows(t, claimID)

	e := echo.New()
	req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"reason":"sequenced"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("claim_id", "id")
	ctx.SetParamValues(claimID.String(), grantID.String())
	ctx.Set("app_id", appID)
	ctx.Set("request_id", uuid.New())
	if err := smw.SyncSeq(pecho.PatchClaimUser)(ctx); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("patch grant: %v %d %s", err, rec.Code, rec.Body.String())
	}

	got, err := client.ParseSyncCursor(rec.Header().Get(client.HeaderSyncSeq))
	if err != nil {
		t.Fatalf("a write must answer with %s, got %q", client.HeaderSyncSeq, rec.Header().Get(client.HeaderSyncSeq))
	}
	var own client.SyncCursor
	if err := psdb.Instance().NewRaw(`
		SELECT txid::TEXT::BIGINT AS txid, seq FROM claimius.sync_event
		WHERE app_id = ? AND event_type = 'claimius.user_claim' AND payload ->> 'reason' = 'sequenced'
		ORDER BY txid DESC, seq DESC LIMIT 1
	`, appID).Scan(context.Background(), &own); err != nil {
		t.Fatalf("read the write's event: %v", err)
	}
	if got.Less(own) || got != getAppHead(t, appID) {
		t.Fatalf("the answer must carry the app's position covering the write's event %s, got %s", own, got)
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
	if seqs := getPageSeqs(t, rec); len(seqs) != 1 || !head.Less(seqs[0]) {
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
	below := client.SyncCursor{Txid: head.Txid, Seq: head.Seq - 1}
	getSyncPage(appID, below, "consumer="+second.String())

	var cursors []client.SyncCursor
	if err := psdb.Instance().NewRaw(`
		SELECT last_applied_txid::TEXT::BIGINT AS txid, last_applied_seq AS seq FROM claimius.sync_consumer
		WHERE app_id = ? AND consumer_id IN (?, ?)
		ORDER BY last_applied_txid DESC, last_applied_seq DESC
	`, appID, first, second).Scan(context.Background(), &cursors); err != nil {
		t.Fatalf("read cursors: %v", err)
	}
	if len(cursors) != 2 || cursors[0] != head || cursors[1] != below {
		t.Fatalf("two consumers of one app must keep their own cursors, got %v", cursors)
	}
}

func TestSyncEventsSendsAStaleCursorToTheSnapshot(t *testing.T) {
	appID, _ := getGrantFixture(t)
	var issued int64
	if err := psdb.Instance().NewRaw(`SELECT pg_sequence_last_value('claimius.sync_event_seq_seq')`).Scan(context.Background(), &issued); err != nil {
		t.Fatalf("read sequence: %v", err)
	}

	if rec := getSyncPage(appID, client.SyncCursor{Txid: getAppHead(t, appID).Txid, Seq: issued + 1000}, "wait=10"); rec.Code != http.StatusConflict {
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

func TestSyncEventsServesAConsumerBoundElsewhereOnlyUnderAnAgreedHandover(t *testing.T) {
	appID, _ := getGrantFixture(t)
	head := getAppHead(t, appID)

	rec := getSyncPage(appID, head, "prophet=gotest_old")
	if rec.Code != http.StatusConflict || rec.Header().Get(client.HeaderHome) != "gotest_old" {
		t.Fatalf("a consumer bound to another prophet without a handover must be refused and keep it, got %d %q", rec.Code, rec.Header().Get(client.HeaderHome))
	}
	rec = getSyncPage(appID, head, "prophet=gotest")
	if rec.Code != http.StatusOK || rec.Header().Get(client.HeaderProphet) != "gotest" {
		t.Fatalf("a consumer bound here must be served and told the prophet's slug, got %d %q", rec.Code, rec.Header().Get(client.HeaderProphet))
	}

	var handover uuid.UUID
	if err := psdb.Instance().NewRaw(`
		INSERT INTO claimius.app_handover (app_id, from_prophet, to_prophet, status, sa_created_by)
		VALUES (?, 'gotest_old', 'gotest', 'accepted', ?) RETURNING id
	`, appID, grantUser).Scan(context.Background(), &handover); err != nil {
		t.Fatalf("record handover: %v", err)
	}
	defer psdb.Instance().ExecContext(context.Background(), `DELETE FROM claimius.app_handover WHERE id = ?`, handover)

	if rec := getSyncPage(appID, getAppHead(t, appID), "prophet=gotest_old"); rec.Code != http.StatusOK {
		t.Fatalf("a consumer of the prophet that handed the app over must be served once the handover is agreed, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestAppHomeRefusesAWriteForAnAppAnotherProphetDecides(t *testing.T) {
	appID, _ := getGrantFixture(t)
	ctx := context.Background()
	if _, err := psdb.Instance().ExecContext(ctx, `UPDATE claimius.samna_app SET home_prophet = 'gotest_elsewhere' WHERE id = ?`, otherApp); err != nil {
		t.Fatalf("mirror app: %v", err)
	}
	defer psdb.Instance().ExecContext(ctx, `UPDATE claimius.samna_app SET home_prophet = NULL WHERE id = ?`, otherApp)

	write := func(method string, app uuid.UUID) (*httptest.ResponseRecorder, bool) {
		reached := false
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(httptest.NewRequest(method, "/", nil), rec)
		c.Set("app_id", app)
		c.Set("request_id", uuid.New())
		_ = smw.AppHome(func(c echo.Context) error { reached = true; return c.NoContent(http.StatusOK) })(c)
		return rec, reached
	}

	if rec, reached := write(http.MethodPost, otherApp); reached || rec.Code != http.StatusConflict || rec.Header().Get(client.HeaderHome) != "gotest_elsewhere" {
		t.Fatalf("a write for an app a peer decides must be refused naming the peer, got %d %q reached=%v", rec.Code, rec.Header().Get(client.HeaderHome), reached)
	}
	if _, reached := write(http.MethodGet, otherApp); !reached {
		t.Fatal("a read of a mirrored app must pass")
	}
	if _, reached := write(http.MethodPatch, appID); !reached {
		t.Fatal("a write for an app decided here must pass")
	}
}

func TestEndStreamsAnswersAHeldPullAtOnce(t *testing.T) {
	appID, _ := getGrantFixture(t)
	answered := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		answered <- getSyncPage(appID, getAppHead(t, appID), "wait=20")
	}()
	select {
	case rec := <-answered:
		t.Fatalf("the pull answered before EndStreams: %d %s", rec.Code, rec.Body.String())
	case <-time.After(500 * time.Millisecond):
	}
	pecho.EndStreams()
	select {
	case rec := <-answered:
		if rec.Code != http.StatusOK || len(getPageSeqs(t, rec)) != 0 {
			t.Fatalf("an ended pull must answer empty: %d %s", rec.Code, rec.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("the held pull is still waiting 2s after EndStreams")
	}
}

func postSyncEventOfType(t *testing.T, appID uuid.UUID, eventType string) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := psdb.Instance().ExecContext(context.Background(), `
		INSERT INTO claimius.sync_event (operation, event_type, payload, app_id)
		VALUES ('upsert', ?, jsonb_build_object('id', ?::TEXT, 'app_id', ?::UUID), ?)
	`, eventType, id, appID, appID); err != nil {
		t.Fatalf("write %s event: %v", eventType, err)
	}
	return id
}

func getSyncConsumer(t *testing.T, appID, consumerID uuid.UUID) shared.SyncConsumer {
	t.Helper()
	consumers, err := psdb.GetSyncConsumers(context.Background(), appID)
	if err != nil {
		t.Fatalf("read sync consumers: %v", err)
	}
	for _, c := range consumers {
		if c.ConsumerID == consumerID {
			return c
		}
	}
	t.Fatalf("consumer %s is not among the app's consumers %+v", consumerID, consumers)
	return shared.SyncConsumer{}
}

func TestGetSyncConsumersNamesWhereEachConsumerStands(t *testing.T) {
	appID, _ := getGrantFixture(t)
	ctx := context.Background()
	start := getAppHead(t, appID)
	disciple, peer, current := uuid.New(), uuid.New(), uuid.New()

	getSyncPage(appID, start, "consumer="+disciple.String())
	if _, err := psdb.Instance().ExecContext(ctx, `SELECT claimius.update_sync_cursor(?, ?, ?, ?, TRUE)`, appID, peer, start.Txid, start.Seq); err != nil {
		t.Fatalf("record peer cursor: %v", err)
	}
	handover := postSyncEventOfType(t, appID, "claimius.app_handover")
	field := postSyncEventOfType(t, appID, "claimius.user_field")
	head := getAppHead(t, appID)
	getSyncPage(appID, head, "consumer="+current.String())

	d := getSyncConsumer(t, appID, disciple)
	if d.Peer || d.Txid != start.Txid || d.Seq != start.Seq || !d.InContact {
		t.Fatalf("a disciple must read with the cursor it pulled at and in contact, got %+v", d)
	}
	if d.HeadTxid != head.Txid || d.HeadSeq != head.Seq {
		t.Fatalf("every consumer must carry the app's head %s, got %d.%d", head, d.HeadTxid, d.HeadSeq)
	}
	if d.Behind != 1 || d.NextEventType == nil || *d.NextEventType != "claimius.user_field" || d.NextPayloadID == nil || *d.NextPayloadID != field ||
		d.NextOperation == nil || *d.NextOperation != "upsert" || d.NextSeq == nil || *d.NextSeq != head.Seq {
		t.Fatalf("a disciple must count and name only what the feed serves it, the handover left out, got %+v", d)
	}

	p := getSyncConsumer(t, appID, peer)
	if !p.Peer || p.Behind != 2 || p.NextEventType == nil || *p.NextEventType != "claimius.app_handover" || p.NextPayloadID == nil || *p.NextPayloadID != handover {
		t.Fatalf("a peer must count and name the handover the feed serves it, got %+v", p)
	}

	c := getSyncConsumer(t, appID, current)
	if c.Behind != 0 || c.NextSeq != nil || c.NextEventType != nil || c.NextPayloadID != nil {
		t.Fatalf("a consumer at the head has nothing after its cursor, got %+v", c)
	}

	var trimmed client.SyncCursor
	if err := psdb.Instance().NewRaw(`SELECT trimmed_txid::TEXT::BIGINT AS txid, trimmed_seq AS seq FROM claimius.prophet_state WHERE app_id = ?`, appID).Scan(ctx, &trimmed); err != nil {
		t.Fatalf("read trimmed position: %v", err)
	}
	if c.TrimmedTxid != trimmed.Txid || c.TrimmedSeq != trimmed.Seq {
		t.Fatalf("every consumer must carry the app's trimmed position %s, got %d.%d", trimmed, c.TrimmedTxid, c.TrimmedSeq)
	}

	if _, err := psdb.Instance().ExecContext(ctx, `UPDATE claimius.sync_consumer SET last_contact_at = now() - INTERVAL '8 days' WHERE app_id = ? AND consumer_id = ?`, appID, disciple); err != nil {
		t.Fatalf("age consumer: %v", err)
	}
	if d := getSyncConsumer(t, appID, disciple); d.InContact {
		t.Fatalf("a consumer out of contact for more than 7 days must read as out of contact, got %+v", d)
	}
}

func TestGetSyncConsumersBoundsItsCountAndDescendsOncePerConsumer(t *testing.T) {
	appID, _ := getGrantFixture(t)
	ctx := context.Background()
	start := getAppHead(t, appID)
	consumer := uuid.New()
	getSyncPage(appID, start, "consumer="+consumer.String())
	if _, err := psdb.Instance().ExecContext(ctx, `
		INSERT INTO claimius.sync_event (operation, event_type, payload, app_id)
		SELECT 'upsert', 'claimius.user_field', jsonb_build_object('id', gen_random_uuid()), ?
		FROM generate_series(1, 20000)
	`, appID); err != nil {
		t.Fatalf("write events: %v", err)
	}
	if c := getSyncConsumer(t, appID, consumer); c.Behind != 1001 || c.NextSeq == nil || *c.NextSeq <= start.Seq {
		t.Fatalf("a consumer more than 1,000 events behind must count 1,001 and name its next event, got %+v", c)
	}

	if _, err := psdb.Instance().ExecContext(ctx, `ANALYZE claimius.sync_event`); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	consumers, err := psdb.GetSyncConsumers(ctx, appID)
	if err != nil {
		t.Fatalf("read sync consumers: %v", err)
	}
	var plan []map[string]any
	var raw []byte
	if err := psdb.Instance().NewRaw(`EXPLAIN (ANALYZE, FORMAT JSON) SELECT * FROM claimius.get_sync_consumers(?)`, appID).Scan(ctx, &raw); err != nil {
		t.Fatalf("explain: %v", err)
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("decode plan %s: %v", raw, err)
	}
	var scans []string
	var loops []float64
	var walk func(node map[string]any)
	walk = func(node map[string]any) {
		if rel, _ := node["Relation Name"].(string); rel == "sync_event" {
			index, _ := node["Index Name"].(string)
			scans = append(scans, node["Node Type"].(string)+" "+index)
			n, _ := node["Actual Loops"].(float64)
			loops = append(loops, n)
		}
		children, _ := node["Plans"].([]any)
		for _, child := range children {
			walk(child.(map[string]any))
		}
	}
	walk(plan[0]["Plan"].(map[string]any))
	for _, scan := range scans {
		if !strings.HasSuffix(scan, " idx_sync_event_app_txid_seq") {
			t.Fatalf("every read of sync_event must be a descent of idx_sync_event_app_txid_seq, got %v", scans)
		}
	}
	if len(scans) != 2 || loops[0]+loops[1] != float64(1+len(consumers)) {
		t.Fatalf("the read must descend once for the head and once per consumer of %d, got %v loops %v", len(consumers), scans, loops)
	}
}
