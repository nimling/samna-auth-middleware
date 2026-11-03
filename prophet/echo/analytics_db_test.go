//go:build dbtest

package echo_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/analytics"
	"github.com/nimling/samna-auth-middleware/v2/notification"
	psam "github.com/nimling/samna-auth-middleware/v2/prophet/sam"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/smw"
)

func TestProphetInitRunsNotificationAnalyticsRetentionAndPeerSync(t *testing.T) {
	t.Setenv("SAUTH_DEBUG_USER_ID", "")
	t.Setenv("SAUTH_DEBUG_CLIENTS", "")
	db, err := psdb.Init(echo.New().Logger)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	var peerID uuid.UUID
	if err := db.NewRaw(`
		INSERT INTO claimius.prophet_peer (prophet, endpoint, app_slug, app_secret, status, sa_created_by)
		VALUES ('gotest-peer', 'http://127.0.0.1:1', ?, 'secret', 'suspended', ?) RETURNING id
	`, "gotest-peer-"+uuid.NewString(), grantUser).Scan(t.Context(), &peerID); err != nil {
		t.Fatalf("peer row: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM claimius.prophet_peer WHERE id = ?`, peerID); err != nil {
			t.Errorf("delete peer row: %v", err)
		}
	})
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()
	tryLock := func(key string) bool {
		var held bool
		if err := conn.QueryRowContext(t.Context(), "SELECT pg_try_advisory_lock(hashtextextended(?, 0))", key).Scan(&held); err != nil {
			t.Fatalf("try lock: %v", err)
		}
		if held {
			if _, err := conn.ExecContext(t.Context(), "SELECT pg_advisory_unlock(hashtextextended(?, 0))", key); err != nil {
				t.Fatalf("unlock: %v", err)
			}
		}
		return held
	}
	locks := []string{"claimius.analytics_fold", shared.RetentionLockKey, "claimius.prophet_peer:" + peerID.String()}
	ctx, cancel := context.WithCancel(t.Context())
	if err := psam.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := psam.Init(ctx); !errors.Is(err, psam.ErrStarted) {
		t.Fatalf("second init = %v, want ErrStarted", err)
	}
	for _, key := range locks {
		deadline := time.Now().Add(15 * time.Second)
		for tryLock(key) {
			if time.Now().After(deadline) {
				t.Fatalf("%s never took its leader lock", key)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	for name, done := range map[string]<-chan struct{}{"notification": notification.Done(), "analytics": analytics.Done()} {
		select {
		case <-done:
			t.Fatalf("%s did not start with sam.Init", name)
		default:
		}
	}
	cancel()
	select {
	case <-psam.Done():
	case <-time.After(15 * time.Second):
		t.Fatalf("Done not closed after cancel")
	}
	for name, done := range map[string]<-chan struct{}{"notification": notification.Done(), "analytics": analytics.Done()} {
		select {
		case <-done:
		default:
			t.Fatalf("sam.Done closed before %s ended", name)
		}
	}
	for _, key := range locks {
		if !tryLock(key) {
			t.Fatalf("%s kept its leader lock after the end", key)
		}
	}
}

func getAnalyticsReader(t *testing.T) (*httptest.Server, peerApp, uuid.UUID) {
	t.Helper()
	getGrantFixture(t)
	ctx := context.Background()
	db := psdb.Instance()
	app := postPeerApp(t, grantUser)
	analyst := uuid.New()
	if _, err := db.ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Analyst')`, analyst, app.ID); err != nil {
		t.Fatalf("analyst: %v", err)
	}
	var claimID uuid.UUID
	if err := db.NewRaw(`SELECT (claimius.insert_claim(
		p_app_id => ?, p_name => 'Analytics reader', p_description => NULL, p_sa_access => 4,
		p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?
	) -> 'claim' ->> 'id')::UUID`, app.ID, app.Org, app.Org, grantUser).Scan(ctx, &claimID); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := db.ExecContext(ctx, `SELECT claimius.insert_claim_object(
		p_app_id => ?, p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?,
		p_sa_access => 4, p_scope => '{"analytics": {}}'::JSONB
	)`, app.ID, claimID, app.Org, app.Org, grantUser); err != nil {
		t.Fatalf("binding: %v", err)
	}
	if _, err := db.ExecContext(ctx, `SELECT claimius.insert_user_claim(p_app_id => ?, p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?)`,
		app.ID, claimID, analyst, app.Org, grantUser); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if _, err := db.ExecContext(ctx, `SELECT claimius.upsert_analytics_capture(?, '/', 'application/json', '{}'::BYTEA)`, app.ID); err != nil {
		t.Fatalf("capture: %v", err)
	}

	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("request_id", uuid.New())
			return next(c)
		}
	})
	psam.RegisterEndpoints(e.Group("/sam", smw.AppMiddleware))
	psam.RegisterCaptureRead(e.Group("/api", func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("app_id", app.ID)
			if raw := c.Request().Header.Get("X-Test-Session"); raw != "" {
				c.Set("user_id", uuid.MustParse(raw))
			}
			return next(c)
		}
	}))
	server := httptest.NewServer(e)
	t.Cleanup(server.Close)
	return server, app, analyst
}

func getAnalyticsRead(t *testing.T, url string, header map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return res.StatusCode, string(body)
}

func getAnalyticsRoutes(appID uuid.UUID) map[string]string {
	app := appID.String()
	return map[string]string{
		"get_analytics_apps":     "/analytics/app",
		"get_analytics_events":   "/analytics/event?app=" + app,
		"get_analytics_sessions": "/analytics/session?app=" + app,
		"get_analytics_capture":  "/analytics/capture?app=" + app + "&path=/",
		"get_analytics_heat":     "/analytics/heat?app=" + app + "&path=/",
		"get_analytics_summary":  "/analytics/summary?app=" + app,
	}
}

func TestAnalyticsReadRefusesAForwardedUserWithoutAnAuthenticatedApp(t *testing.T) {
	server, app, analyst := getAnalyticsReader(t)
	for name, route := range getAnalyticsRoutes(app.ID) {
		join := "?"
		if strings.Contains(route, "?") {
			join = "&"
		}
		if code, body := getAnalyticsRead(t, server.URL+"/api"+route+join+"user_id="+analyst.String(), nil); code != http.StatusForbidden {
			t.Errorf("%s with no session and a user_id query: want 403, got %d %s", name, code, body)
		}
		if code, body := getAnalyticsRead(t, server.URL+"/api"+route, map[string]string{"SAuth-Forwarded-User": analyst.String()}); code != http.StatusForbidden {
			t.Errorf("%s with no session and a forwarded user: want 403, got %d %s", name, code, body)
		}
		if code, body := getAnalyticsRead(t, server.URL+"/sam"+route, map[string]string{"SAuth-App-Slug": app.Slug, "SAuth-App-Secret": "wrong", "SAuth-Forwarded-User": analyst.String()}); code != http.StatusUnauthorized {
			t.Errorf("%s forwarded by an app with a wrong secret: want 401, got %d %s", name, code, body)
		}
	}
}

func TestAnalyticsReadServesTheSignedInUserAndTheForwardingApp(t *testing.T) {
	server, app, analyst := getAnalyticsReader(t)
	for name, route := range getAnalyticsRoutes(app.ID) {
		if code, body := getAnalyticsRead(t, server.URL+"/api"+route, map[string]string{"X-Test-Session": analyst.String()}); code != http.StatusOK {
			t.Errorf("%s by the signed in reader: want 200, got %d %s", name, code, body)
		}
		if code, body := getAnalyticsRead(t, server.URL+"/sam"+route, map[string]string{"SAuth-App-Slug": app.Slug, "SAuth-App-Secret": app.Secret, "SAuth-Forwarded-User": analyst.String()}); code != http.StatusOK {
			t.Errorf("%s forwarded by the authenticated app: want 200, got %d %s", name, code, body)
		}
	}
	if _, body := getAnalyticsRead(t, server.URL+"/api/analytics/app", map[string]string{"X-Test-Session": analyst.String()}); !strings.Contains(body, app.ID.String()) {
		t.Fatalf("the signed in reader does not see the app it reads: %s", body)
	}
}

func TestAnalyticsReadKeepsASignedInUserToTheirOwnAnalytics(t *testing.T) {
	server, app, analyst := getAnalyticsReader(t)
	stranger := uuid.New()
	header := map[string]string{"X-Test-Session": stranger.String(), "SAuth-Forwarded-User": analyst.String()}
	query := "user_id=" + analyst.String()

	code, body := getAnalyticsRead(t, server.URL+"/api/analytics/app?"+query, header)
	if code != http.StatusOK || strings.Contains(body, app.ID.String()) {
		t.Fatalf("a signed in user naming another reader lists that reader's apps: %d %s", code, body)
	}
	if code, body := getAnalyticsRead(t, server.URL+"/api/analytics/capture?app="+app.ID.String()+"&path=/&"+query, header); code != http.StatusNotFound {
		t.Fatalf("a signed in user naming another reader reads that reader's capture: %d %s", code, body)
	}
}
