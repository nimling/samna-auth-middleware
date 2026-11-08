package echo_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	saecho "github.com/nimling/samna-auth-middleware/v2/disciple/echo"
	"github.com/nimling/samna-auth-middleware/v2/uptime"
)

func statusStub(t *testing.T, path *string, headers *http.Header, status int, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*path = r.URL.Path
		*headers = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write stub body: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SAUTH_ENDPOINT", srv.URL)
	t.Setenv("SAUTH_APP_SLUG", "testapp")
	t.Setenv("SAUTH_APP_SECRET", "secret")
}

func TestGetUptimeStatusProxiesThePublicStatusOfTheOwnApp(t *testing.T) {
	var path string
	var headers http.Header
	statusStub(t, &path, &headers, http.StatusOK, `{"data":{"app_slug":"testapp","app_name":"Test App","monitors":[{"id":"6f1d4d1e-6a1a-4a53-9a9e-9b0f0a2f6a11","name":"heartbeat","kind":"heartbeat","interval_seconds":60,"enabled":true,"last_ok":true}],"incidents":[]},"count":1}`)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/uptime/status", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := saecho.GetUptimeStatus(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if path != "/api/app/testapp/status" {
		t.Fatalf("forward path = %q", path)
	}
	if got := headers.Get("SAuth-App-Slug"); got != "testapp" {
		t.Fatalf("SAuth-App-Slug = %q", got)
	}
	var out struct {
		Data  uptime.MonitorStatus `json:"data"`
		Count int                  `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Data.AppSlug != "testapp" || out.Data.AppName != "Test App" || out.Count != 1 {
		t.Fatalf("envelope = %+v", out)
	}
	if len(out.Data.Monitors) != 1 || out.Data.Monitors[0].Kind != uptime.MonitorKindHeartbeat || out.Data.Monitors[0].LastOK == nil || !*out.Data.Monitors[0].LastOK {
		t.Fatalf("monitors = %+v", out.Data.Monitors)
	}
}

func TestGetUptimeStatusAnswersBadGatewayWhenTheProphetFails(t *testing.T) {
	var path string
	var headers http.Header
	statusStub(t, &path, &headers, http.StatusInternalServerError, `{"data":[],"error":{"message":"status query failed"},"count":0}`)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/uptime/status", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := saecho.GetUptimeStatus(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestGetUptimeAnswersUnavailableBeforeTheFirstBeat(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/uptime", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := saecho.GetUptime(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
