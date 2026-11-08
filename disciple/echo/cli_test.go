package echo_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	saecho "github.com/nimling/samna-auth-middleware/v2/disciple/echo"
)

const cliChallenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

type fakeProphet struct {
	paths  []string
	forms  []url.Values
	slugs  []string
	status int
	body   string
}

func (p *fakeProphet) serve(t *testing.T) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		p.paths = append(p.paths, r.Method+" "+r.URL.Path)
		p.forms = append(p.forms, r.Form)
		p.slugs = append(p.slugs, r.Header.Get("SAuth-App-Slug")+":"+r.Header.Get("SAuth-App-Secret"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(p.status)
		w.Write([]byte(p.body))
	}))
	t.Cleanup(server.Close)
	t.Setenv("SAUTH_ENDPOINT", server.URL)
	t.Setenv("SAUTH_APP_SLUG", "testapp")
	t.Setenv("SAUTH_APP_SECRET", "app-secret")
	t.Setenv("SAUTH_REDIRECT_URI", "http://svc.example/api/auth/callback")
	t.Setenv("SAUTH_DEBUG_USER_ID", "")
	t.Setenv("SAUTH_DEBUG_CLIENTS", "")
}

func callCli(t *testing.T, method string, target string, form url.Values, cookies []*http.Cookie, handler echo.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	body := strings.NewReader("")
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationForm)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	if err := handler(echo.New().NewContext(req, rec)); err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	return rec
}

func getResponseCookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestGetLoginSendsTheClisChallengeAndKeepsNoVerifier(t *testing.T) {
	(&fakeProphet{status: http.StatusOK}).serve(t)

	rec := callCli(t, http.MethodGet, "/api/auth/login?cli_challenge="+cliChallenge+"&cli_redirect="+url.QueryEscape("http://127.0.0.1:53682/callback")+"&cli_state=abcdefghijklmnop", nil, nil, saecho.GetLogin)

	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil || rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d location = %q", rec.Code, rec.Header().Get("Location"))
	}
	if location.Query().Get("code_challenge") != cliChallenge {
		t.Errorf("code_challenge = %q, want the cli's own", location.Query().Get("code_challenge"))
	}
	if location.Query().Get("redirect_uri") != "http://svc.example/api/auth/callback" {
		t.Errorf("redirect_uri = %q, want the service's registered callback", location.Query().Get("redirect_uri"))
	}
	if c := getResponseCookie(rec, client.PKCEVerifierCookie); c != nil && c.Value != "" {
		t.Errorf("verifier cookie = %q, want none: only the cli holds the verifier", c.Value)
	}
	if c := getResponseCookie(rec, client.CliTargetCookie); c == nil || c.Value != "http://127.0.0.1:53682/callback?state=abcdefghijklmnop" {
		t.Errorf("cli target cookie = %v, want the loopback with the cli state", c)
	}
}

func TestGetLoginRefusesANonLoopbackRedirect(t *testing.T) {
	(&fakeProphet{status: http.StatusOK}).serve(t)

	for _, redirect := range []string{"https://evil.example/callback", "http://127.0.0.1/callback", "http://user@127.0.0.1:8080/", "http://10.0.0.1:8080/"} {
		rec := callCli(t, http.MethodGet, "/api/auth/login?cli_challenge="+cliChallenge+"&cli_redirect="+url.QueryEscape(redirect)+"&cli_state=abcdefghijklmnop", nil, nil, saecho.GetLogin)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("cli_redirect %q: status = %d, want 400", redirect, rec.Code)
		}
	}
}

func TestGetLoginWithoutCliClearsAStaleCliTarget(t *testing.T) {
	(&fakeProphet{status: http.StatusOK}).serve(t)

	rec := callCli(t, http.MethodGet, "/api/auth/login", nil, []*http.Cookie{{Name: client.CliTargetCookie, Value: "paste"}}, saecho.GetLogin)

	if c := getResponseCookie(rec, client.CliTargetCookie); c == nil || c.MaxAge >= 0 {
		t.Errorf("cli target cookie = %v, want it cleared so a browser login is never handed to a cli", c)
	}
	if c := getResponseCookie(rec, client.PKCEVerifierCookie); c == nil || c.Value == "" {
		t.Errorf("verifier cookie missing on a browser login")
	}
}

func TestGetCallbackHandsTheCodeToTheLoopbackWithoutExchanging(t *testing.T) {
	prophet := &fakeProphet{status: http.StatusOK, body: `{}`}
	prophet.serve(t)

	cookies := []*http.Cookie{{Name: client.OAuthStateCookie, Value: "st1"}, {Name: client.CliTargetCookie, Value: "http://127.0.0.1:53682/callback?state=abcdefghijklmnop"}}
	rec := callCli(t, http.MethodGet, "/api/auth/callback?code=thecode&state=st1", nil, cookies, saecho.GetCallback)

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "http://127.0.0.1:53682/callback?code=thecode&state=abcdefghijklmnop" {
		t.Fatalf("status = %d location = %q", rec.Code, rec.Header().Get("Location"))
	}
	if len(prophet.paths) != 0 {
		t.Errorf("prophet calls = %v, want none: the cli exchanges the code with its verifier", prophet.paths)
	}
	if c := getResponseCookie(rec, "sauth_access_token"); c != nil && c.Value != "" {
		t.Errorf("access cookie written on a cli login")
	}
}

func TestGetCallbackShowsTheCodeToPasteWhenTheCliHasNoLoopback(t *testing.T) {
	(&fakeProphet{status: http.StatusOK, body: `{}`}).serve(t)

	cookies := []*http.Cookie{{Name: client.OAuthStateCookie, Value: "st1"}, {Name: client.CliTargetCookie, Value: "paste"}}
	rec := callCli(t, http.MethodGet, "/api/auth/callback?code=thecode&state=st1", nil, cookies, saecho.GetCallback)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "thecode") {
		t.Fatalf("status = %d, want the paste page with the code", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Errorf("headers = %v", rec.Header())
	}
}

func TestGetCallbackRefusesACliLoginWithAForeignState(t *testing.T) {
	(&fakeProphet{status: http.StatusOK, body: `{}`}).serve(t)

	cookies := []*http.Cookie{{Name: client.OAuthStateCookie, Value: "st1"}, {Name: client.CliTargetCookie, Value: "paste"}}
	rec := callCli(t, http.MethodGet, "/api/auth/callback?code=thecode&state=other", nil, cookies, saecho.GetCallback)

	if rec.Code != http.StatusFound || strings.Contains(rec.Body.String(), "thecode") {
		t.Errorf("status = %d, want the login started over without showing the code", rec.Code)
	}
}

func TestPostCliTokenExchangesTheCodeWithTheClisVerifier(t *testing.T) {
	prophet := &fakeProphet{status: http.StatusOK, body: `{"data":{"access_token":"acc","refresh_token":"ref","access_expires_in":900,"refresh_expires_in":2592000,"token_type":"Bearer"},"count":1}`}
	prophet.serve(t)

	rec := callCli(t, http.MethodPost, "/api/auth/cli/token", url.Values{"grant_type": {"authorization_code"}, "code": {"thecode"}, "code_verifier": {"theverifier"}}, nil, saecho.PostCliToken)

	var got saecho.ResponseCliToken
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if got.AccessToken != "acc" || got.RefreshToken != "ref" || got.ExpiresIn != 900 || got.TokenType != "Bearer" {
		t.Errorf("token = %+v", got)
	}
	form := prophet.forms[0]
	if prophet.paths[0] != "POST /api/app/testapp/oauth/token" || form.Get("code") != "thecode" || form.Get("code_verifier") != "theverifier" || form.Get("redirect_uri") != "http://svc.example/api/auth/callback" {
		t.Errorf("prophet call = %v %v", prophet.paths, form)
	}
	if prophet.slugs[0] != "testapp:app-secret" {
		t.Errorf("prophet credentials = %q, want the app's own", prophet.slugs[0])
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
}

func TestPostCliTokenRelaysTheProphetsRefusal(t *testing.T) {
	(&fakeProphet{status: http.StatusBadRequest, body: `{"data":{"error":"invalid_grant","error_description":"code_verifier does not match code_challenge"},"count":1}`}).serve(t)

	rec := callCli(t, http.MethodPost, "/api/auth/cli/token", url.Values{"grant_type": {"authorization_code"}, "code": {"thecode"}, "code_verifier": {"wrong"}}, nil, saecho.PostCliToken)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"error":"invalid_grant"`) {
		t.Errorf("status = %d body = %s, want 400 invalid_grant", rec.Code, rec.Body.String())
	}
}

func TestPostCliTokenRefusesAnUnknownGrant(t *testing.T) {
	prophet := &fakeProphet{status: http.StatusOK, body: `{}`}
	prophet.serve(t)

	rec := callCli(t, http.MethodPost, "/api/auth/cli/token", url.Values{"grant_type": {"password"}}, nil, saecho.PostCliToken)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "unsupported_grant_type") || len(prophet.paths) != 0 {
		t.Errorf("status = %d body = %s calls = %v", rec.Code, rec.Body.String(), prophet.paths)
	}
}

func TestPostCliRevokeRevokesTheClisRefreshFamily(t *testing.T) {
	prophet := &fakeProphet{status: http.StatusOK}
	prophet.serve(t)

	rec := callCli(t, http.MethodPost, "/api/auth/cli/revoke", url.Values{"token": {"ref"}, "token_type_hint": {"refresh_token"}}, nil, saecho.PostCliRevoke)

	if rec.Code != http.StatusOK || prophet.paths[0] != "POST /api/app/testapp/oauth/revoke" || prophet.forms[0].Get("token") != "ref" {
		t.Errorf("status = %d calls = %v forms = %v", rec.Code, prophet.paths, prophet.forms)
	}
}

func TestPostCliTokenRelaysADeadRefreshTokenAsInvalidGrant(t *testing.T) {
	(&fakeProphet{status: http.StatusUnauthorized, body: `{"data":{"error":"invalid_grant","error_description":"refresh token expired"},"count":1}`}).serve(t)

	rec := callCli(t, http.MethodPost, "/api/auth/cli/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"dead"}}, nil, saecho.PostCliToken)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"error":"invalid_grant"`) {
		t.Errorf("status = %d body = %s, want 400 invalid_grant so the cli asks to log in again", rec.Code, rec.Body.String())
	}
}

func TestPostCliTokenHidesTheServicesOwnClientFailure(t *testing.T) {
	(&fakeProphet{status: http.StatusUnauthorized, body: `{"error":"invalid_client","error_description":"invalid app secret"}`}).serve(t)

	rec := callCli(t, http.MethodPost, "/api/auth/cli/token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"any"}}, nil, saecho.PostCliToken)

	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), `"error":"server_error"`) {
		t.Errorf("status = %d body = %s, want 502 server_error: the service's own secret is not the cli's fault", rec.Code, rec.Body.String())
	}
}
