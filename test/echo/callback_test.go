package echo

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	saecho "github.com/nimling/samna-auth-middleware/echo"
)

func TestGetCallbackSetsTokenAndCSRFCookies(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/app/testapp/oauth/token" {
			t.Errorf("unexpected token path %s", r.URL.Path)
		}
		if r.Header.Get("SAuth-App-Slug") != "testapp" || r.Header.Get("SAuth-App-Secret") != "secret" {
			t.Errorf("missing app credentials on token exchange")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"access_token":"tok","refresh_token":"ref","token_type":"Bearer","access_expires_in":3600,"refresh_expires_in":2592000},"count":1,"request_id":"00000000-0000-0000-0000-000000000000"}`))
	}))
	defer tokenServer.Close()

	t.Setenv("SAUTH_ENDPOINT", tokenServer.URL)
	t.Setenv("SAUTH_APP_SLUG", "testapp")
	t.Setenv("SAUTH_APP_SECRET", "secret")
	t.Setenv("SAUTH_REDIRECT_URI", "http://localhost:4200/api/sauth/callback")
	t.Setenv("SAUTH_PAGE_REDIRECT", "http://localhost:4200/")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/sauth/callback?code=abc&state=st1", nil)
	req.AddCookie(&http.Cookie{Name: "sauth_oauth_state", Value: "st1"})
	req.AddCookie(&http.Cookie{Name: "sauth_pkce_verifier", Value: "ver1"})
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := saecho.GetCallback(ctx); err != nil {
		t.Fatalf("GetCallback: %v", err)
	}

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}

	got := map[string]string{}
	for _, c := range rec.Result().Cookies() {
		got[c.Name] = c.Value
	}
	if got["sauth_access_token"] != "tok" {
		t.Errorf("sauth_access_token = %q, want tok", got["sauth_access_token"])
	}
	if got["sauth_refresh_token"] != "ref" {
		t.Errorf("sauth_refresh_token = %q, want ref", got["sauth_refresh_token"])
	}
	if got["sauth_csrf_token"] == "" {
		t.Errorf("sauth_csrf_token not set on login callback")
	}
}

func TestGetCallbackRejectsStateMismatch(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/sauth/callback?code=abc&state=other", nil)
	req.AddCookie(&http.Cookie{Name: "sauth_oauth_state", Value: "st1"})
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := saecho.GetCallback(ctx); err != nil {
		t.Fatalf("GetCallback: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 on state mismatch", rec.Code)
	}
}
