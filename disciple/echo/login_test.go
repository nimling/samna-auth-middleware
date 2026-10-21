package echo_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	saecho "github.com/nimling/samna-auth-middleware/v2/disciple/echo"
)

func getLoginNext(t *testing.T, target string, referer string) string {
	t.Helper()
	t.Setenv("SAUTH_ENDPOINT", "http://auth.example")
	t.Setenv("SAUTH_APP_SLUG", "testapp")
	t.Setenv("SAUTH_REDIRECT_URI", "http://localhost:4200/api/auth/callback")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Host = "localhost:4200"
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := saecho.GetLogin(ctx); err != nil {
		t.Fatalf("GetLogin: %v", err)
	}
	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "sauth_login_next" {
			return c.Value
		}
	}
	return ""
}

func TestGetLoginRemembersNextQuery(t *testing.T) {
	if got := getLoginNext(t, "/api/auth/login?next=%2Fsettings%3Ftab%3Dkeys", "http://localhost:4200/other"); got != "/settings?tab=keys" {
		t.Errorf("sauth_login_next = %q, want /settings?tab=keys", got)
	}
}

func TestGetLoginRemembersSameOriginReferer(t *testing.T) {
	if got := getLoginNext(t, "/api/auth/login", "http://localhost:4200/connection/42?tab=runs"); got != "/connection/42?tab=runs" {
		t.Errorf("sauth_login_next = %q, want /connection/42?tab=runs", got)
	}
}

func TestGetLoginIgnoresForeignReferer(t *testing.T) {
	if got := getLoginNext(t, "/api/auth/login", "http://evil.example/steal"); got != "" {
		t.Errorf("sauth_login_next = %q, want empty", got)
	}
}

func TestGetLoginIgnoresProtocolRelativeNext(t *testing.T) {
	if got := getLoginNext(t, "/api/auth/login?next=%2F%2Fevil.example", ""); got != "" {
		t.Errorf("sauth_login_next = %q, want empty", got)
	}
}

func TestGetLoginCarriesNextInTheState(t *testing.T) {
	t.Setenv("SAUTH_ENDPOINT", "http://auth.example")
	t.Setenv("SAUTH_APP_SLUG", "testapp")
	t.Setenv("SAUTH_REDIRECT_URI", "http://localhost:4200/api/auth/callback")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/login?next=%2Fsettings%3Ftab%3Dkeys", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := saecho.GetLogin(ctx); err != nil {
		t.Fatalf("GetLogin: %v", err)
	}
	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("Location: %v", err)
	}
	if state := location.Query().Get("state"); !strings.HasSuffix(state, ".L3NldHRpbmdzP3RhYj1rZXlz") {
		t.Errorf("state = %q, want the next path encoded after the dot", state)
	}
}

func TestGetLoginAuthorizesAChildApp(t *testing.T) {
	t.Setenv("SAUTH_ENDPOINT", "http://auth.example")
	t.Setenv("SAUTH_APP_SLUG", "testapp")
	t.Setenv("SAUTH_REDIRECT_URI", "http://localhost:4200/api/auth/callback")
	t.Setenv("SAUTH_DEBUG_USER_ID", "a0000000-0000-4000-8000-00000000000a")
	t.Setenv("SAUTH_DEBUG_APPS", "testapp-web=http://web.example")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/login?app=testapp-web&next=%2Fbookings", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := saecho.GetLogin(ctx); err != nil {
		t.Fatalf("GetLogin: %v", err)
	}
	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307", rec.Code)
	}
	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	if location.Path != "/api/app/testapp-web/oauth/authorize" || location.Query().Get("client_id") != "testapp-web" {
		t.Errorf("location = %q, want the child app's authorize", location)
	}
	if location.Query().Get("redirect_uri") != "http://localhost:4200/api/auth/callback" {
		t.Errorf("redirect_uri = %q, want the disciple callback", location.Query().Get("redirect_uri"))
	}
	app := ""
	for _, c := range rec.Result().Cookies() {
		if c.Name == "sauth_login_app" {
			app = c.Value
		}
	}
	if app != "testapp-web" {
		t.Errorf("sauth_login_app = %q, want testapp-web", app)
	}
}

func TestGetLoginRefusesAnUnknownApp(t *testing.T) {
	t.Setenv("SAUTH_ENDPOINT", "http://auth.example")
	t.Setenv("SAUTH_APP_SLUG", "testapp")
	t.Setenv("SAUTH_REDIRECT_URI", "http://localhost:4200/api/auth/callback")
	t.Setenv("SAUTH_DEBUG_USER_ID", "a0000000-0000-4000-8000-00000000000a")
	t.Setenv("SAUTH_DEBUG_APPS", "testapp-web=http://web.example")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/login?app=otherapp", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := saecho.GetLogin(ctx); err != nil {
		t.Fatalf("GetLogin: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
