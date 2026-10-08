package bff_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/bff"
	"github.com/nimling/samna-auth-middleware/v2/client"
)

type handoffCapture struct {
	prophetHost    string
	host           string
	forwardedHost  string
	forwardedProto string
	path           string
	query          string
}

func handoffProphet(t *testing.T) *handoffCapture {
	t.Helper()
	capture := &handoffCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.host = r.Host
		capture.forwardedHost = r.Header.Get(bff.HeaderXForwardedHost)
		capture.forwardedProto = r.Header.Get(echo.HeaderXForwardedProto)
		capture.path = r.URL.Path
		capture.query = r.URL.RawQuery
		w.Header().Add(echo.HeaderSetCookie, "sauth_access_token=access; Path=/; Domain=docs.example; HttpOnly")
		w.Header().Add(echo.HeaderSetCookie, "sauth_app_slug=docs; Path=/; Domain=docs.example")
		w.Header().Set(echo.HeaderLocation, "/guarded/page")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srv.Close)
	capture.prophetHost = srv.Listener.Addr().String()
	t.Setenv("SAUTH_ENDPOINT", srv.URL)
	t.Setenv("SAUTH_APP_SLUG", "docs")
	t.Setenv("SAUTH_APP_SECRET", "secret")
	return capture
}

func TestGetSessionProxiesTheHandoffToTheProphetWithTheBrowserHostForwarded(t *testing.T) {
	capture := handoffProphet(t)
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/session?handoff=abc123&next=%2Fguarded%2Fpage", nil)
	req.Host = "docs.example:4300"
	req.Header.Set(echo.HeaderXForwardedProto, "https")
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := bff.GetSession(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}

	if capture.host != capture.prophetHost {
		t.Fatalf("proxied host = %q, want the prophet host %q", capture.host, capture.prophetHost)
	}
	if capture.forwardedHost != "docs.example:4300" {
		t.Fatalf("x-forwarded-host = %q, want the browser host", capture.forwardedHost)
	}
	if capture.forwardedProto != "https" {
		t.Fatalf("x-forwarded-proto = %q, want https", capture.forwardedProto)
	}
	if capture.path != "/api/auth/session" {
		t.Fatalf("forwarded path = %q", capture.path)
	}
	if capture.query != "handoff=abc123&next=%2Fguarded%2Fpage" {
		t.Fatalf("forwarded query = %q", capture.query)
	}
	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want 307", rec.Code)
	}
	if location := rec.Header().Get(echo.HeaderLocation); location != "/guarded/page" {
		t.Fatalf("location = %q, want /guarded/page", location)
	}
	cookies := rec.Header().Values(echo.HeaderSetCookie)
	if len(cookies) != 2 {
		t.Fatalf("set-cookie count = %d, want 2", len(cookies))
	}
	if cookies[0] != "sauth_access_token=access; Path=/; Domain=docs.example; HttpOnly" {
		t.Fatalf("first cookie = %q", cookies[0])
	}
	if cookies[1] != "sauth_app_slug=docs; Path=/; Domain=docs.example" {
		t.Fatalf("second cookie = %q", cookies[1])
	}
}

func TestGetSessionAnswersBadGatewayWithoutAProphetEndpoint(t *testing.T) {
	t.Setenv("SAUTH_ENDPOINT", "")
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/session?handoff=abc123", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := bff.GetSession(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestGetSessionSendsTheVisitorToThePageTheLoginStartedFrom(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(echo.HeaderLocation, "/")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SAUTH_ENDPOINT", srv.URL)
	t.Setenv("SAUTH_APP_SLUG", "docs")
	t.Setenv("SAUTH_APP_SECRET", "secret")

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/session?handoff=abc123", nil)
	req.AddCookie(&http.Cookie{Name: client.LoginNextCookie, Value: "/test"})
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := bff.GetSession(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if location := rec.Header().Get(echo.HeaderLocation); location != "/test" {
		t.Fatalf("location = %q, want /test", location)
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == client.LoginNextCookie && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("the login next cookie was not cleared")
	}
}

func TestGetSessionKeepsTheProphetRedirectForAnOffSiteLoginNext(t *testing.T) {
	handoffProphet(t)
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/session?handoff=abc123", nil)
	req.AddCookie(&http.Cookie{Name: client.LoginNextCookie, Value: "//evil.example"})
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)

	if err := bff.GetSession(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if location := rec.Header().Get(echo.HeaderLocation); location != "/guarded/page" {
		t.Fatalf("location = %q, want the prophet's /guarded/page", location)
	}
}
