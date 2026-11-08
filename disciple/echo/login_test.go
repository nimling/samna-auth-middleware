package echo_test

import (
	"net/http"
	"net/http/httptest"
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
	if got := getLoginNext(t, "/api/auth/login?next=%2Fapi%2Fauth%2Fcli%3Fcode%3Dabc", "http://localhost:4200/other"); got != "/api/auth/cli?code=abc" {
		t.Errorf("sauth_login_next = %q, want /api/auth/cli?code=abc", got)
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
