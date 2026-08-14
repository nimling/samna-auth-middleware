package echo

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	saecho "github.com/nimling/samna-auth-middleware/disciple/echo"
)

func captureReadStub(t *testing.T, path *string, query *string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*path = r.URL.Path
		*query = r.URL.Query().Encode()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"data":[],"count":0}`)); err != nil {
			t.Errorf("write stub body: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SAUTH_ENDPOINT", srv.URL)
	t.Setenv("SAUTH_APP_SLUG", "testapp")
	t.Setenv("SAUTH_APP_SECRET", "secret")
}

func TestGetAnalyticsEventsForwardsTheTargetAppAndRange(t *testing.T) {
	var path, query string
	captureReadStub(t, &path, &query)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/sauth/analytics/event?app=6f1d4d1e-6a1a-4a53-9a9e-9b0f0a2f6a11&from=2026-08-01T00:00:00Z&to=2026-08-14T00:00:00Z&type=click&nope=1", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := saecho.GetAnalyticsEvents(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if path != "/sam/analytics/event" {
		t.Fatalf("forward path = %q", path)
	}
	want := "app=6f1d4d1e-6a1a-4a53-9a9e-9b0f0a2f6a11&from=2026-08-01T00%3A00%3A00Z&to=2026-08-14T00%3A00%3A00Z&type=click"
	if query != want {
		t.Fatalf("forward query = %q, want %q", query, want)
	}
}

func TestGetAnalyticsSessionsForwardsTheTargetApp(t *testing.T) {
	var path, query string
	captureReadStub(t, &path, &query)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/sauth/analytics/session?app=6f1d4d1e-6a1a-4a53-9a9e-9b0f0a2f6a11&limit=50", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := saecho.GetAnalyticsSessions(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if path != "/sam/analytics/session" {
		t.Fatalf("forward path = %q", path)
	}
	want := "app=6f1d4d1e-6a1a-4a53-9a9e-9b0f0a2f6a11&limit=50"
	if query != want {
		t.Fatalf("forward query = %q, want %q", query, want)
	}
}
