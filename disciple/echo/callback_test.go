package echo_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	saecho "github.com/nimling/samna-auth-middleware/v2/disciple/echo"
	"github.com/nimling/samna-auth-middleware/v2/shared"
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
		w.Write([]byte(`{"access_token":"tok","refresh_token":"ref","token_type":"Bearer","expires_in":3600,"refresh_expires_in":2592000}`))
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
	req := httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=abc&state=st1.L3NldHRpbmdzP3RhYj1rZXlz", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := saecho.GetCallback(ctx); err != nil {
		t.Fatalf("GetCallback: %v", err)
	}
	if location := rec.Header().Get("Location"); location != "/api/auth/login?next=%2Fsettings%3Ftab%3Dkeys" {
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

func TestGetCallbackReturnsToTheInitiateApp(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/app/testapp/oauth/token" {
			t.Errorf("token path = %s, want the root app's", r.URL.Path)
		}
		if r.Header.Get("SAuth-App-Slug") != "testapp" || r.Header.Get("SAuth-App-Secret") != "secret" {
			t.Errorf("the initiate app's code is not exchanged with the root app's credentials")
		}
		if err := r.ParseForm(); err != nil || r.PostForm.Get("redirect_uri") != "http://localhost:4200/api/auth/callback?app=testapp-web" {
			t.Errorf("redirect_uri = %q, want the initiate app's callback", r.PostForm.Get("redirect_uri"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"tok","refresh_token":"ref","token_type":"Bearer","expires_in":3600,"refresh_expires_in":2592000}`))
	}))
	defer tokenServer.Close()

	t.Setenv("SAUTH_ENDPOINT", tokenServer.URL)
	t.Setenv("SAUTH_APP_SLUG", "testapp")
	t.Setenv("SAUTH_APP_SECRET", "secret")
	t.Setenv("SAUTH_REDIRECT_URI", "http://localhost:4200/api/auth/callback")
	t.Setenv("SAUTH_PAGE_REDIRECT", "http://localhost:4200/")
	t.Setenv("SAUTH_DEBUG_USER_ID", "a0000000-0000-4000-8000-00000000000a")
	t.Setenv("SAUTH_DEBUG_INITIATE_APPS", "testapp-web=http://web.example/")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/callback?app=testapp-web&code=abc&state=st1", nil)
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
	if location := rec.Header().Get("Location"); location != "http://web.example/" {
		t.Errorf("location = %q, want http://web.example/", location)
	}
}

func TestGetLogoutEndsTheInitiateAppSession(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer authServer.Close()

	t.Setenv("SAUTH_ENDPOINT", authServer.URL)
	t.Setenv("SAUTH_APP_SLUG", "testapp")
	t.Setenv("SAUTH_APP_SECRET", "secret")
	t.Setenv("SAUTH_PAGE_REDIRECT", "http://localhost:4200/")
	t.Setenv("SAUTH_DEBUG_USER_ID", "a0000000-0000-4000-8000-00000000000a")
	t.Setenv("SAUTH_DEBUG_INITIATE_APPS", "testapp-web=http://web.example")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/testapp-web/logout", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("app")
	ctx.SetParamValues("testapp-web")

	if err := saecho.GetLogout(ctx); err != nil {
		t.Fatalf("GetLogout: %v", err)
	}
	location := rec.Header().Get("Location")
	if !strings.Contains(location, "/api/app/testapp-web/oauth/end_session") {
		t.Errorf("location = %q, want the initiate app's end_session", location)
	}
	if !strings.Contains(location, "post_logout_redirect_uri=http%3A%2F%2Fweb.example") {
		t.Errorf("location = %q, want the initiate app's origin", location)
	}
}

func TestGetCallbackSetsTheInitiateAppCookieScopeFromTheProphet(t *testing.T) {
	prophet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/sam/app/initiate":
			w.Write([]byte(`{"data":[{"slug":"testapp-web","redirect_uris":["https://web.example","https://web.example/api/auth/callback?app=testapp-web"],"cookie_domain":"","cookie_same_site":"none"}],"count":1,"request_id":"00000000-0000-0000-0000-000000000000"}`))
		case "/api/app/testapp/oauth/token":
			if err := r.ParseForm(); err != nil || r.PostForm.Get("redirect_uri") != "https://web.example/api/auth/callback?app=testapp-web" {
				t.Errorf("redirect_uri = %q, want the initiate app's registered callback", r.PostForm.Get("redirect_uri"))
			}
			w.Write([]byte(`{"access_token":"tok","refresh_token":"ref","token_type":"Bearer","expires_in":3600,"refresh_expires_in":2592000}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer prophet.Close()

	t.Setenv("SAUTH_ENDPOINT", prophet.URL)
	t.Setenv("SAUTH_APP_SLUG", "testapp")
	t.Setenv("SAUTH_APP_SECRET", "secret")
	t.Setenv("SAUTH_REDIRECT_URI", "https://api.example/api/auth/callback")
	t.Setenv("SAUTH_COOKIE_DOMAIN", "api.example")
	t.Setenv("SAUTH_DEBUG_USER_ID", "")
	t.Setenv("SAUTH_DEBUG_CLIENTS", "")

	if _, err := shared.PutInitiateApps(context.Background()); err != nil {
		t.Fatalf("PutInitiateApps: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/callback?app=testapp-web&code=abc&state=st1", nil)
	req.AddCookie(&http.Cookie{Name: "sauth_oauth_state", Value: "st1"})
	req.AddCookie(&http.Cookie{Name: "sauth_pkce_verifier", Value: "ver1"})
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := saecho.GetCallback(ctx); err != nil {
		t.Fatalf("GetCallback: %v", err)
	}
	if location := rec.Header().Get("Location"); location != "https://web.example" {
		t.Errorf("location = %q, want https://web.example", location)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name != "sauth_access_token" {
			continue
		}
		if c.Domain != "" || c.SameSite != http.SameSiteNoneMode || !c.Secure {
			t.Errorf("sauth_access_token domain=%q samesite=%v secure=%v, want host only, none, secure", c.Domain, c.SameSite, c.Secure)
		}
	}
}

func getCallbackLocation(t *testing.T, target string, next string) string {
	t.Helper()
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"tok","refresh_token":"ref","token_type":"Bearer","expires_in":3600,"refresh_expires_in":2592000}`))
	}))
	t.Cleanup(tokenServer.Close)

	t.Setenv("SAUTH_ENDPOINT", tokenServer.URL)
	t.Setenv("SAUTH_APP_SLUG", "testapp")
	t.Setenv("SAUTH_APP_SECRET", "secret")
	t.Setenv("SAUTH_REDIRECT_URI", "https://api.example/api/auth/callback")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.AddCookie(&http.Cookie{Name: "sauth_oauth_state", Value: "st1"})
	req.AddCookie(&http.Cookie{Name: "sauth_pkce_verifier", Value: "ver1"})
	if next != "" {
		req.AddCookie(&http.Cookie{Name: "sauth_login_next", Value: next})
	}
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := saecho.GetCallback(ctx); err != nil {
		t.Fatalf("GetCallback: %v", err)
	}
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	return rec.Header().Get("Location")
}

func TestGetCallbackReturnsANextPathOnThePageRedirectOrigin(t *testing.T) {
	t.Setenv("SAUTH_PAGE_REDIRECT", "https://portal.example")
	if location := getCallbackLocation(t, "/api/auth/callback?code=abc&state=st1", "/dashboard?tab=1"); location != "https://portal.example/dashboard?tab=1" {
		t.Errorf("Location = %q, want the path on the page redirect origin", location)
	}
}

func TestGetCallbackReturnsAnAbsoluteNextOnThePageRedirectOrigin(t *testing.T) {
	t.Setenv("SAUTH_PAGE_REDIRECT", "https://portal.example")
	if location := getCallbackLocation(t, "/api/auth/callback?code=abc&state=st1", "https://portal.example/dashboard"); location != "https://portal.example/dashboard" {
		t.Errorf("Location = %q, want https://portal.example/dashboard", location)
	}
}

func TestGetCallbackFallsBackToThePageRedirectForAForeignNext(t *testing.T) {
	t.Setenv("SAUTH_PAGE_REDIRECT", "https://portal.example")
	if location := getCallbackLocation(t, "/api/auth/callback?code=abc&state=st1", "https://evil.example/dashboard"); location != "https://portal.example" {
		t.Errorf("Location = %q, want the page redirect", location)
	}
}

func TestGetCallbackReturnsANextPathOnTheInitiateAppOrigin(t *testing.T) {
	t.Setenv("SAUTH_PAGE_REDIRECT", "https://portal.example")
	t.Setenv("SAUTH_DEBUG_USER_ID", "a0000000-0000-4000-8000-00000000000a")
	t.Setenv("SAUTH_DEBUG_INITIATE_APPS", "testapp-web=https://web.example/")
	if location := getCallbackLocation(t, "/api/auth/callback?app=testapp-web&code=abc&state=st1", "/bookings"); location != "https://web.example/bookings" {
		t.Errorf("Location = %q, want the path on the initiate app origin", location)
	}
}
