package echo_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	saecho "github.com/nimling/samna-auth-middleware/v2/disciple/echo"
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
	t.Setenv("SAUTH_REDIRECT_URI", "http://localhost:4200/api/auth/callback")
	t.Setenv("SAUTH_PAGE_REDIRECT", "http://localhost:4200/")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=abc&state=st1", nil)
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
	t.Setenv("SAUTH_PAGE_REDIRECT", "http://localhost:4200/")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=abc&state=other", nil)
	req.AddCookie(&http.Cookie{Name: "sauth_oauth_state", Value: "st1"})
	req.AddCookie(&http.Cookie{Name: "sauth_pkce_verifier", Value: "ver1"})
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := saecho.GetCallback(ctx); err != nil {
		t.Fatalf("GetCallback: %v", err)
	}
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 on state mismatch", rec.Code)
	}
	if location := rec.Header().Get("Location"); location != "http://localhost:4200/" {
		t.Fatalf("Location = %q, want the page redirect so the visitor starts over", location)
	}

	cleared := map[string]int{}
	for _, c := range rec.Result().Cookies() {
		cleared[c.Name] = c.MaxAge
	}
	if cleared["sauth_oauth_state"] != -1 {
		t.Errorf("sauth_oauth_state MaxAge = %d, want -1 so the stale pair cannot fail again", cleared["sauth_oauth_state"])
	}
	if cleared["sauth_pkce_verifier"] != -1 {
		t.Errorf("sauth_pkce_verifier MaxAge = %d, want -1 so the stale pair cannot fail again", cleared["sauth_pkce_verifier"])
	}
}

func TestGetLogoutForwardsTokenAndClearsCookies(t *testing.T) {
	var gotPath, gotAuth, gotSlug, gotSecret string
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotSlug = r.Header.Get("SAuth-App-Slug")
		gotSecret = r.Header.Get("SAuth-App-Secret")
		w.WriteHeader(http.StatusOK)
	}))
	defer authServer.Close()

	t.Setenv("SAUTH_ENDPOINT", authServer.URL)
	t.Setenv("SAUTH_APP_SLUG", "testapp")
	t.Setenv("SAUTH_APP_SECRET", "secret")
	t.Setenv("SAUTH_PAGE_REDIRECT", "http://localhost:4200/")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "sauth_access_token", Value: "tok123"})
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := saecho.GetLogout(ctx); err != nil {
		t.Fatalf("GetLogout: %v", err)
	}

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 to end_session", rec.Code)
	}
	location := rec.Header().Get("Location")
	if !strings.Contains(location, "/api/app/testapp/oauth/end_session") {
		t.Errorf("location = %q, want end_session url", location)
	}
	if !strings.Contains(location, "post_logout_redirect_uri=http%3A%2F%2Flocalhost%3A4200%2F") {
		t.Errorf("location = %q, want post_logout_redirect_uri", location)
	}
	if gotPath != "/sam/logout" {
		t.Errorf("path = %q, want /sam/logout", gotPath)
	}
	if gotAuth != "Bearer tok123" {
		t.Errorf("authorization = %q, want Bearer tok123", gotAuth)
	}
	if gotSlug != "testapp" || gotSecret != "secret" {
		t.Errorf("app credentials missing: slug=%q secret=%q", gotSlug, gotSecret)
	}

	cleared := map[string]bool{}
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 {
			cleared[c.Name] = true
		}
	}
	for _, name := range []string{"sauth_access_token", "sauth_refresh_token", "sauth_csrf_token"} {
		if !cleared[name] {
			t.Errorf("cookie %s not cleared on logout", name)
		}
	}
}

func TestGetLogoutWithoutTokenRedirectsToEndSession(t *testing.T) {
	called := false
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer authServer.Close()

	t.Setenv("SAUTH_ENDPOINT", authServer.URL)
	t.Setenv("SAUTH_APP_SLUG", "testapp")
	t.Setenv("SAUTH_APP_SECRET", "secret")
	t.Setenv("SAUTH_PAGE_REDIRECT", "http://localhost:4200/")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/logout", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := saecho.GetLogout(ctx); err != nil {
		t.Fatalf("GetLogout: %v", err)
	}
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 to end_session", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Location"), "/api/app/testapp/oauth/end_session") {
		t.Errorf("location = %q, want end_session url", rec.Header().Get("Location"))
	}
	if called {
		t.Errorf("auth server called without an access token")
	}
}

func TestGetCallbackStartsOverWhenTheCodeExchangeIsRefused(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer tokenServer.Close()

	t.Setenv("SAUTH_ENDPOINT", tokenServer.URL)
	t.Setenv("SAUTH_APP_SLUG", "testapp")
	t.Setenv("SAUTH_APP_SECRET", "secret")
	t.Setenv("SAUTH_REDIRECT_URI", "http://localhost:4200/api/auth/callback")
	t.Setenv("SAUTH_PAGE_REDIRECT", "http://localhost:4200/")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=used&state=st1", nil)
	req.AddCookie(&http.Cookie{Name: "sauth_oauth_state", Value: "st1"})
	req.AddCookie(&http.Cookie{Name: "sauth_pkce_verifier", Value: "ver1"})
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := saecho.GetCallback(ctx); err != nil {
		t.Fatalf("GetCallback: %v", err)
	}
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 so the visitor starts over instead of reading a json error", rec.Code)
	}
	if location := rec.Header().Get("Location"); location != "http://localhost:4200/" {
		t.Fatalf("Location = %q, want the page redirect", location)
	}
	cleared := map[string]bool{}
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 {
			cleared[c.Name] = true
		}
	}
	if !cleared["sauth_oauth_state"] || !cleared["sauth_pkce_verifier"] {
		t.Fatalf("the flow cookies must be cleared, got %v", rec.Result().Cookies())
	}
}

func TestGetCallbackResumesTheLoginFromTheStateInAnotherBrowser(t *testing.T) {
	t.Setenv("SAUTH_PAGE_REDIRECT", "http://localhost:4200/")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=abc&state=st1.L2NsaT9sb2dpbj1hYmM", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := saecho.GetCallback(ctx); err != nil {
		t.Fatalf("GetCallback: %v", err)
	}
	if location := rec.Header().Get("Location"); location != "/api/auth/login?next=%2Fcli%3Flogin%3Dabc" {
		t.Fatalf("Location = %q, want the login restarted with the next path from the state", location)
	}
}

func TestGetCallbackIgnoresAProtocolRelativeNextInTheState(t *testing.T) {
	t.Setenv("SAUTH_PAGE_REDIRECT", "http://localhost:4200/")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=abc&state=st1.Ly9ldmlsLmV4YW1wbGU", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := saecho.GetCallback(ctx); err != nil {
		t.Fatalf("GetCallback: %v", err)
	}
	if location := rec.Header().Get("Location"); location != "http://localhost:4200/" {
		t.Fatalf("Location = %q, want the page redirect", location)
	}
}
