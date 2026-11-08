package smw_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/smw"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

func newAuthServerWithFailingRefresh(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/jwks") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"keys":[]}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
}

func userAuthHandlerAgainst(t *testing.T, server *httptest.Server, opts *smw.UserAuthOptions) echo.HandlerFunc {
	t.Helper()
	t.Setenv("SAUTH_DEBUG_USER_ID", "")
	t.Setenv("SAUTH_DEBUG_CLIENTS", "")
	t.Setenv("SAUTH_APP_SLUG", "testapp")
	t.Setenv("SAUTH_APP_SECRET", "secret")
	t.Setenv("SAUTH_REDIRECT_URI", "http://localhost:4200/api/auth/callback")

	tokenURL, err := url.Parse(server.URL + "/api/app/testapp/oauth/token")
	if err != nil {
		t.Fatalf("parse token url: %v", err)
	}
	jwksURL, err := url.Parse(server.URL + "/jwks")
	if err != nil {
		t.Fatalf("parse jwks url: %v", err)
	}
	utils.SetOIDC(server.URL, *tokenURL, *jwksURL)

	return smw.UserAuthMiddleware(opts)(func(ctx echo.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})
}

func TestRefreshAuthServerErrorAnswersUnauthorizedOnFetch(t *testing.T) {
	server := newAuthServerWithFailingRefresh(t)
	defer server.Close()

	handler := userAuthHandlerAgainst(t, server, nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/me/employee", nil)
	req.AddCookie(&http.Cookie{Name: "sauth_refresh_token", Value: "ref"})
	rec := httptest.NewRecorder()

	if err := handler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("middleware handler: %v", err)
	}

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestRefreshAuthServerErrorRedirectsToLoginOnNavigation(t *testing.T) {
	server := newAuthServerWithFailingRefresh(t)
	defer server.Close()

	handler := userAuthHandlerAgainst(t, server, nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/nimployment", nil)
	req.Header.Set("Accept", "text/html")
	req.AddCookie(&http.Cookie{Name: "sauth_refresh_token", Value: "ref"})
	rec := httptest.NewRecorder()

	if err := handler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("middleware handler: %v", err)
	}

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/api/auth/login" {
		t.Fatalf("location = %q, want /api/auth/login", loc)
	}
}

func TestOptionalServesRequestCarryingNoCredential(t *testing.T) {
	server := newAuthServerWithFailingRefresh(t)
	defer server.Close()

	handler := userAuthHandlerAgainst(t, server, &smw.UserAuthOptions{Optional: true})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/guide/intro", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()

	if err := handler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("middleware handler: %v", err)
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q, want ok", rec.Body.String())
	}
}

func TestOptionalRefusesRequestCarryingABrokenCredential(t *testing.T) {
	server := newAuthServerWithFailingRefresh(t)
	defer server.Close()

	handler := userAuthHandlerAgainst(t, server, &smw.UserAuthOptions{Optional: true})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/me/employee", nil)
	req.AddCookie(&http.Cookie{Name: "sauth_refresh_token", Value: "ref"})
	rec := httptest.NewRecorder()

	if err := handler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("middleware handler: %v", err)
	}

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestWithoutOptionalARequestCarryingNoCredentialIsRefused(t *testing.T) {
	server := newAuthServerWithFailingRefresh(t)
	defer server.Close()

	handler := userAuthHandlerAgainst(t, server, nil)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/me/employee", nil)
	rec := httptest.NewRecorder()

	if err := handler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("middleware handler: %v", err)
	}

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestARefreshKeepsTheCSRFTokenTheBrowserAlreadyHolds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/jwks") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"keys":[]}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"access_token":"fresh","refresh_token":"next","token_type":"Bearer","access_expires_in":900,"refresh_expires_in":2592000},"count":1}`))
	}))
	defer server.Close()
	handler := userAuthHandlerAgainst(t, server, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/thing", nil)
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.AddCookie(&http.Cookie{Name: "sauth_refresh_token", Value: "old"})
	req.AddCookie(&http.Cookie{Name: "sauth_csrf_token", Value: "held"})
	rec := httptest.NewRecorder()
	_ = handler(echo.New().NewContext(req, rec))

	for _, c := range rec.Result().Cookies() {
		if c.Name == "sauth_csrf_token" {
			if c.Value != "held" {
				t.Fatalf("a refresh must keep the csrf token the browser holds, got %q", c.Value)
			}
			if c.MaxAge <= 0 {
				t.Fatalf("a refresh must extend the csrf cookie, got max age %d", c.MaxAge)
			}
			return
		}
	}
	t.Fatal("a refresh must rewrite the csrf cookie so it outlives a day")
}

func TestNavigationRedirectsToTheLoginBesideTheRedirectURI(t *testing.T) {
	server := newAuthServerWithFailingRefresh(t)
	defer server.Close()

	handler := userAuthHandlerAgainst(t, server, nil)
	t.Setenv("SAUTH_REDIRECT_URI", "https://sign.example.test/auth/callback")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/screens", nil)
	req.Header.Set("Accept", "text/html")
	req.AddCookie(&http.Cookie{Name: "sauth_refresh_token", Value: "ref"})
	rec := httptest.NewRecorder()

	if err := handler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("middleware handler: %v", err)
	}
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusFound || loc != "/auth/login" {
		t.Fatalf("status %d location %q, want 302 /auth/login", rec.Code, loc)
	}
}

func TestGetOriginAllowedAdmitsTheInitiateAppOrigins(t *testing.T) {
	t.Setenv("SAUTH_REDIRECT_URI", "http://localhost:4200/api/auth/callback")
	t.Setenv("SAUTH_DEBUG_USER_ID", "a0000000-0000-4000-8000-00000000000a")
	t.Setenv("SAUTH_DEBUG_INITIATE_APPS", "testapp-web=http://web.example")

	allowed := smw.GetOriginAllowed([]string{"http://portal.example"})
	for origin, want := range map[string]bool{"http://portal.example": true, "http://web.example": true, "http://evil.example": false} {
		if got, _ := allowed(origin); got != want {
			t.Errorf("origin %s allowed = %v, want %v", origin, got, want)
		}
	}
}
