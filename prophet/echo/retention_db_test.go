//go:build dbtest

package echo_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	pecho "github.com/nimling/samna-auth-middleware/v2/prophet/echo"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

type retentionPage struct {
	Data []shared.Retention `json:"data"`
}

type retentionItem struct {
	Data shared.Retention `json:"data"`
}

func getRetentionStatus(handler echo.HandlerFunc, method string, userID uuid.UUID, kind, body string) (int, string) {
	e := echo.New()
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("kind")
	ctx.SetParamValues(kind)
	ctx.Set("user_id", userID)
	ctx.Set("request_id", uuid.New())
	_ = handler(ctx)
	return rec.Code, rec.Body.String()
}

func postSystemClaimHolder(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	db := psdb.Instance()
	owner := uuid.New()
	if _, err := db.ExecContext(ctx, `
		DO $$
		DECLARE
			v_app   UUID := claimius.get_app_id();
			v_claim claimius.claim;
		BEGIN
			SELECT * INTO v_claim FROM claimius.claim
			WHERE app_id = v_app AND type = 'system' AND sa_access & 1 = 1 AND sa_deleted_at IS NULL
			ORDER BY sa_created_at LIMIT 1;
			PERFORM claimius.upsert_app_user(p_user_id => '`+owner.String()+`', p_app_id => v_app, p_first_name => 'Keeper');
			PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_claim.id, p_user_id => '`+owner.String()+`',
				p_sa_owner_id => v_claim.sa_owner_id, p_sa_created_by => v_claim.sa_created_by);
		END $$;
	`); err != nil {
		t.Fatalf("owner grant: %v", err)
	}
	return owner
}

func TestRetentionIsReadAndEditedThroughAccessOnTheOwnerOrganization(t *testing.T) {
	getGrantFixture(t)
	owner := postSystemClaimHolder(t)
	t.Cleanup(func() {
		if _, err := psdb.Instance().ExecContext(context.Background(), `UPDATE claimius.retention SET days = 365 WHERE kind = 'audit'`); err != nil {
			t.Errorf("restore audit days: %v", err)
		}
	})

	if code, body := getRetentionStatus(pecho.GetRetentions, http.MethodGet, grantUser, "", ""); code != http.StatusForbidden {
		t.Fatalf("a user with no read on the owner organization of the system app reads retention: %d %s", code, body)
	}
	if code, body := getRetentionStatus(pecho.PutRetention, http.MethodPut, grantUser, "audit", `{"days": 30}`); code != http.StatusForbidden {
		t.Fatalf("a user with no write on the owner organization of the system app edits retention: %d %s", code, body)
	}

	code, body := getRetentionStatus(pecho.GetRetentions, http.MethodGet, owner, "", "")
	if code != http.StatusOK {
		t.Fatalf("a holder of the system claim on the owner organization reads retention: %d %s", code, body)
	}
	var page retentionPage
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	kinds := map[string]bool{}
	for _, r := range page.Data {
		kinds[r.Kind] = true
	}
	for _, kind := range []string{"access_diff", "sync_event", "audit", "notification", "peer_collision", "analytics_event",
		"analytics_session", "analytics_heat", "analytics_capture", "consent", "rate_limit", "par_request"} {
		if !kinds[kind] {
			t.Fatalf("the prophet keeps no retention for %s: %v", kind, kinds)
		}
	}

	code, body = getRetentionStatus(pecho.PutRetention, http.MethodPut, owner, "audit", `{"days": 400}`)
	if code != http.StatusOK {
		t.Fatalf("a holder of the system claim on the owner organization edits retention: %d %s", code, body)
	}
	var item retentionItem
	if err := json.Unmarshal([]byte(body), &item); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if item.Data.Kind != "audit" || item.Data.Days != 400 {
		t.Fatalf("the edit answers the row as written, got %+v", item.Data)
	}
	if code, body := getRetentionStatus(pecho.PutRetention, http.MethodPut, owner, "audit", `{"days": 0}`); code != http.StatusBadRequest {
		t.Fatalf("a retention below a day is refused: %d %s", code, body)
	}
	if code, body := getRetentionStatus(pecho.PutRetention, http.MethodPut, owner, "gotest_unknown", `{"days": 3}`); code != http.StatusNotFound {
		t.Fatalf("an unknown kind is not found: %d %s", code, body)
	}
}

func TestRetentionDeletesWhatEachKindKeepsNoLonger(t *testing.T) {
	appID, _ := getGrantFixture(t)
	ctx := context.Background()
	db := psdb.Instance()
	old, fresh := uuid.New(), uuid.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := db.NewRaw(query, args...).Scan(ctx, &n); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return n
	}
	deleteExpired := func(kind string) {
		t.Helper()
		if _, err := db.DeleteExpired(ctx, kind); err != nil {
			t.Fatalf("delete expired %s: %v", kind, err)
		}
	}

	exec(`INSERT INTO claimius.rate_limit (key, tat) VALUES (?, now() - INTERVAL '2 days'), (?, now() + INTERVAL '1 hour')`, old.String(), fresh.String())
	deleteExpired("rate_limit")
	if n := count(`SELECT count(*) FROM claimius.rate_limit WHERE key IN (?, ?)`, old.String(), fresh.String()); n != 1 {
		t.Fatalf("rate_limit keeps only the key still limiting, found %d", n)
	}

	exec(`INSERT INTO claimius.par_request (request_uri, payload, expires_at) VALUES (?, '{}', now() - INTERVAL '2 days'), (?, '{}', now() + INTERVAL '1 minute')`, old.String(), fresh.String())
	deleteExpired("par_request")
	if n := count(`SELECT count(*) FROM claimius.par_request WHERE request_uri IN (?, ?)`, old.String(), fresh.String()); n != 1 {
		t.Fatalf("par_request keeps only the request still usable, found %d", n)
	}

	exec(`INSERT INTO claimius.peer_collision (id, prophet, app_slug, object_type, reason, payload, sa_created_at) VALUES
		(?, 'gotest', 'gotest', 'claimius.organization', 'test', '{}', now() - INTERVAL '40 days'),
		(?, 'gotest', 'gotest', 'claimius.organization', 'test', '{}', now())`, old, fresh)
	deleteExpired("peer_collision")
	if n := count(`SELECT count(*) FROM claimius.peer_collision WHERE id IN (?, ?)`, old, fresh); n != 1 {
		t.Fatalf("peer_collision keeps only the recent refusal, found %d", n)
	}

	exec(`INSERT INTO claimius.notification (id, app_id, user_id, title, type, sa_deleted_at) VALUES
		(?, ?, ?, 'old', 'test', now() - INTERVAL '100 days'), (?, ?, ?, 'fresh', 'test', now())`, old, appID, grantUser, fresh, appID, grantUser)
	deleteExpired("notification")
	if n := count(`SELECT count(*) FROM claimius.notification WHERE id IN (?, ?)`, old, fresh); n != 1 {
		t.Fatalf("notification keeps only the row deleted within the retention, found %d", n)
	}

	exec(`INSERT INTO claimius.analytics_session (id, app_id, anonymous_id, session_key, last_seen_at) VALUES
		(?, ?, 'gotest', ?, now() - INTERVAL '40 days'), (?, ?, 'gotest', ?, now())`, old, appID, old.String(), fresh, appID, fresh.String())
	exec(`INSERT INTO claimius.analytics_event (app_id, session_id, type, path, occurred_at) VALUES
		(?, ?, 'click', '/gotest', now() - INTERVAL '40 days'), (?, ?, 'click', '/gotest', now())`, appID, old, appID, fresh)
	deleteExpired("analytics_event")
	if n := count(`SELECT count(*) FROM claimius.analytics_event WHERE session_id IN (?, ?)`, old, fresh); n != 1 {
		t.Fatalf("analytics_event keeps only the days within the retention, found %d", n)
	}
	deleteExpired("analytics_session")
	if n := count(`SELECT count(*) FROM claimius.analytics_session WHERE id IN (?, ?)`, old, fresh); n != 1 {
		t.Fatalf("analytics_session keeps only the session still seen or still referred to, found %d", n)
	}

	exec(`INSERT INTO claimius.analytics_heat (app_id, path, viewport_width, type, bucket_x, bucket_y, day) VALUES
		(?, ?, 1, 'click', 0, 0, current_date - 400), (?, ?, 1, 'click', 0, 0, current_date)`, appID, old.String(), appID, fresh.String())
	deleteExpired("analytics_heat")
	if n := count(`SELECT count(*) FROM claimius.analytics_heat WHERE path IN (?, ?)`, old.String(), fresh.String()); n != 1 {
		t.Fatalf("analytics_heat keeps only the days within the retention, found %d", n)
	}

	exec(`INSERT INTO claimius.analytics_capture (app_id, path, content_type, body, captured_at) VALUES
		(?, ?, 'text/html', '\x00', now() - INTERVAL '100 days'), (?, ?, 'text/html', '\x00', now())`, appID, old.String(), appID, fresh.String())
	deleteExpired("analytics_capture")
	if n := count(`SELECT count(*) FROM claimius.analytics_capture WHERE path IN (?, ?)`, old.String(), fresh.String()); n != 1 {
		t.Fatalf("analytics_capture keeps only the recent snapshot, found %d", n)
	}

	exec(`INSERT INTO claimius.consent (id, app_id, anonymous_id, policy_version, revoked_at) VALUES
		(?, ?, ?, '1', now() - INTERVAL '400 days'), (?, ?, ?, '1', NULL)`, old, appID, old.String(), fresh, appID, fresh.String())
	if _, err := db.DeleteExpired(ctx, "consent"); err == nil {
		t.Fatalf("consent is never deleted by age")
	}
	sink := &coldSink{objects: map[string][]byte{}}
	db.ColdStorage = sink
	t.Cleanup(func() { db.ColdStorage = nil })
	deleteExported := func(kind string) {
		t.Helper()
		if _, err := db.DeleteExported(ctx, kind); err != nil {
			t.Fatalf("delete exported %s: %v", kind, err)
		}
	}
	deleteExported("consent")
	if n := count(`SELECT count(*) FROM claimius.consent WHERE id IN (?, ?)`, old, fresh); n != 1 {
		t.Fatalf("consent keeps only the live decision, found %d", n)
	}

	exec(`SELECT claimius.upsert_audit_partition(now() - INTERVAL '3 years')`)
	exec(`INSERT INTO claimius.audit (id, app_id, operation, type, object_type, object_id, sa_owner_id, sa_created_at) VALUES
		(?, ?, 'update', 'test', 'claimius.organization', ?, ?, now() - INTERVAL '3 years'),
		(?, ?, 'update', 'test', 'claimius.organization', ?, ?, now())`, old, appID, grantRoot, grantRoot, fresh, appID, grantRoot, grantRoot)
	deleteExported("audit")
	if n := count(`SELECT count(*) FROM claimius.audit WHERE id IN (?, ?)`, old, fresh); n != 1 {
		t.Fatalf("audit keeps only the months within the retention, found %d", n)
	}
	if n := count(`SELECT count(*) FROM pg_class WHERE relname = 'audit_' || to_char((now() - INTERVAL '3 years') AT TIME ZONE 'UTC', 'YYYYMM')`); n != 0 {
		t.Fatalf("the expired audit month is dropped as a partition")
	}
	if n := count(`SELECT count(*) FROM claimius.retention_export WHERE kind IN ('audit', 'consent')`); n != len(sink.objects) || n < 2 {
		t.Fatalf("each batch written to cold storage has one manifest row, %d rows for %d objects", n, len(sink.objects))
	}

	deleteExpired("sync_event")
	if n := count(`SELECT count(*) FROM claimius.retention WHERE kind = 'sync_event' AND pruned_at IS NOT NULL`); n != 1 {
		t.Fatalf("a run records when it finished")
	}
	if _, err := db.DeleteExpired(ctx, "gotest_unknown"); err == nil {
		t.Fatalf("a kind with no retention is an error, not a silent skip")
	}
}

func TestRetentionTrimsTheAreasOfAnAppADiscipleIsPullingAndOfAnAppWithNoPublishingState(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	db := psdb.Instance()
	app := postPeerApp(t, grantUser)
	postPeerClaim(t, db, app.ID, app.Org, grantUser, "Trimmed claim")
	if _, err := db.ExecContext(ctx, `SELECT claimius.update_sync_cursor(?, ?, 0, 0)`, app.ID, uuid.New()); err != nil {
		t.Fatalf("disciple contact: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM claimius.prophet_state WHERE app_id = ?`, app.ID); err != nil {
		t.Fatalf("drop publishing state: %v", err)
	}
	var areas int
	if err := db.NewRaw(`SELECT count(*) FROM claimius.access_diff WHERE app_id = ?`, app.ID).Scan(ctx, &areas); err != nil {
		t.Fatalf("count areas: %v", err)
	}
	if areas == 0 {
		t.Fatalf("the claim raised no area to trim")
	}
	if _, err := db.DeleteExpired(ctx, "access_diff"); err != nil {
		t.Fatalf("trim: %v", err)
	}
	if err := db.NewRaw(`SELECT count(*) FROM claimius.access_diff WHERE app_id = ?`, app.ID).Scan(ctx, &areas); err != nil {
		t.Fatalf("count areas: %v", err)
	}
	if areas != 0 {
		t.Fatalf("an app with no reader keeps %d areas after the trim", areas)
	}
	var trimmed string
	if err := db.NewRaw(`SELECT access_trimmed_txid::TEXT FROM claimius.prophet_state WHERE app_id = ?`, app.ID).Scan(ctx, &trimmed); err != nil {
		t.Fatalf("the trim records its position for an app with no publishing state: %v", err)
	}
	if trimmed == "0" {
		t.Fatalf("the trimmed position stays at zero")
	}
}

func TestADrainStepThatFailsLeavesTheOthersToRunAndIsReported(t *testing.T) {
	getGrantFixture(t)
	db := psdb.Instance()
	rollback := errors.New("rollback")
	err := db.RunInTx(context.Background(), nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			CREATE OR REPLACE FUNCTION claimius._settle_usage_diff(p_app_id UUID, p_user_id UUID DEFAULT NULL, p_limit INTEGER DEFAULT NULL)
			RETURNS INTEGER AS $$ BEGIN RAISE EXCEPTION 'gotest usage fold'; END; $$ LANGUAGE plpgsql
		`); err != nil {
			t.Fatalf("break the usage fold: %v", err)
		}
		var failures []string
		if err := tx.NewRaw(`SELECT failures FROM claimius.drain_access_diff(10)`).Scan(ctx, pgdialect.Array(&failures)); err != nil {
			t.Fatalf("a failing step fails the drain: %v", err)
		}
		if len(failures) != 1 || !strings.Contains(failures[0], "gotest usage fold") {
			t.Fatalf("the failing step is reported and the others are not, got %v", failures)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("tx: %v", err)
	}
}

func TestAnEventTimeOutsideTheLastDayIsClampedToIt(t *testing.T) {
	appID, _ := getGrantFixture(t)
	ctx := context.Background()
	db := psdb.Instance()
	anon, key := uuid.NewString(), uuid.NewString()
	if _, err := db.ExecContext(ctx, `SELECT claimius.insert_consent(?, ?, '{"analytics": true}', '1')`, appID, anon); err != nil {
		t.Fatalf("consent: %v", err)
	}
	if _, err := db.ExecContext(ctx, `SELECT claimius.upsert_analytics_session(?, ?, ?)`, appID, anon, key); err != nil {
		t.Fatalf("session: %v", err)
	}
	if _, err := db.ExecContext(ctx, `SELECT claimius.insert_analytics_events(?, ?, '[{"type": "click", "occurred_at": 1000}, {"type": "click", "occurred_at": 99999999999999}, {"type": "click"}]')`, appID, key); err != nil {
		t.Fatalf("events: %v", err)
	}
	var outside int
	if err := db.NewRaw(`
		SELECT count(*) FROM claimius.analytics_event e JOIN claimius.analytics_session s ON s.id = e.session_id
		WHERE s.session_key = ? AND (e.occurred_at < now() - INTERVAL '1 day 1 minute' OR e.occurred_at > now())
	`, key).Scan(ctx, &outside); err != nil {
		t.Fatalf("read events: %v", err)
	}
	if outside != 0 {
		t.Fatalf("%d events kept a time outside the last day", outside)
	}
}

type coldSink struct {
	objects map[string][]byte
}

func (s *coldSink) PutObject(_ context.Context, path string, body []byte) error {
	s.objects[path] = body
	return nil
}
