package echo

import (
	_ "embed"
	"errors"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"regexp"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

const (
	CliTokenPath        = "/api/auth/cli/token"
	CliRevokePath       = "/api/auth/cli/revoke"
	cliTargetPaste      = "paste"
	cliGrantAuthCode    = "authorization_code"
	cliGrantRefresh     = "refresh_token"
	cliDebugRefusal     = "this server runs in debug mode and authenticates every request as the debug user, so there is no cli to sign in"
	cliTargetRefusal    = "cli_challenge must be an S256 challenge, cli_redirect a loopback address with a port and cli_state set with it"
	cliLoopbackScheme   = "http"
	cliLoopbackIPv4     = "127.0.0.1"
	cliLoopbackIPv6     = "::1"
	cliLoopbackHostname = "localhost"
)

var cliChallengePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

var cliStatePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

//go:embed cli/code.html
var cliCodeSource string

var cliCodeTemplate = template.Must(template.New("code").Parse(cliCodeSource))

type ResponseCliToken struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshToken     string `json:"refresh_token,omitempty"`
	RefreshExpiresIn int    `json:"refresh_expires_in,omitempty"`
}

type ResponseOAuthError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

type TemplateCliCode struct {
	App  string
	Code string
}

func PostCliToken(ctx echo.Context) error {
	if shared.GetDebugMode() != shared.DebugModeNone {
		return respondOAuthError(ctx, http.StatusBadRequest, "invalid_request", cliDebugRefusal)
	}
	if err := ctx.Request().ParseForm(); err != nil {
		return respondOAuthError(ctx, http.StatusBadRequest, "invalid_request", "malformed token request")
	}
	slug := utils.Cfg().AppSlug
	secret := utils.Cfg().AppSecret
	tokenURL, err := client.TokenURLFor(slug)
	if err != nil {
		ctx.Logger().Errorf("PostCliToken url: %v", err)
		return respondOAuthError(ctx, http.StatusInternalServerError, "server_error", "")
	}

	var tokens *client.ResponseToken
	switch ctx.FormValue("grant_type") {
	case cliGrantAuthCode:
		tokens, err = client.PostAuthCode(ctx.Request().Context(), tokenURL, slug, secret, ctx.FormValue("code"), utils.Cfg().RedirectURI, ctx.FormValue("code_verifier"))
	case cliGrantRefresh:
		tokens, err = client.PostRefreshToken(ctx.Request().Context(), tokenURL, slug, secret, ctx.FormValue("refresh_token"))
	default:
		return respondOAuthError(ctx, http.StatusBadRequest, "unsupported_grant_type", "")
	}
	if err != nil {
		var tokenErr *client.TokenError
		if errors.As(err, &tokenErr) && tokenErr.Status < http.StatusInternalServerError && tokenErr.Code != "invalid_client" {
			return respondOAuthError(ctx, http.StatusBadRequest, tokenErr.Code, tokenErr.Description)
		}
		ctx.Logger().Errorf("PostCliToken: %v", err)
		return respondOAuthError(ctx, http.StatusBadGateway, "server_error", "")
	}

	setNoStore(ctx)
	return ctx.JSON(http.StatusOK, ResponseCliToken{
		AccessToken:      tokens.AccessToken,
		TokenType:        "Bearer",
		ExpiresIn:        tokens.AccessExpiresIn,
		RefreshToken:     tokens.RefreshToken,
		RefreshExpiresIn: tokens.RefreshExpiresIn,
	})
}

func PostCliRevoke(ctx echo.Context) error {
	if err := ctx.Request().ParseForm(); err != nil {
		return respondOAuthError(ctx, http.StatusBadRequest, "invalid_request", "malformed revocation request")
	}
	token := ctx.FormValue("token")
	if token == "" || ctx.FormValue("token_type_hint") == "access_token" {
		return ctx.NoContent(http.StatusOK)
	}
	slug := utils.Cfg().AppSlug
	revokeURL, err := client.RevokeURLFor(slug)
	if err != nil {
		ctx.Logger().Errorf("PostCliRevoke url: %v", err)
		return respondOAuthError(ctx, http.StatusInternalServerError, "server_error", "")
	}
	if err := client.PostRevoke(ctx.Request().Context(), revokeURL, slug, utils.Cfg().AppSecret, token); err != nil {
		ctx.Logger().Errorf("PostCliRevoke: %v", err)
		return respondOAuthError(ctx, http.StatusServiceUnavailable, "temporarily_unavailable", "")
	}
	return ctx.NoContent(http.StatusOK)
}

func getCliTarget(challenge string, redirect string, state string) (string, bool) {
	if !cliChallengePattern.MatchString(challenge) {
		return "", false
	}
	if redirect == "" {
		return cliTargetPaste, true
	}
	if !cliStatePattern.MatchString(state) {
		return "", false
	}
	target, ok := getCliLoopback(redirect)
	if !ok {
		return "", false
	}
	q := target.Query()
	q.Set("state", state)
	target.RawQuery = q.Encode()
	return target.String(), true
}

func getCliLoopback(redirect string) (*url.URL, bool) {
	target, err := url.Parse(redirect)
	if err != nil || target.Scheme != cliLoopbackScheme || target.User != nil || target.Fragment != "" || target.Port() == "" {
		return nil, false
	}
	host := target.Hostname()
	if host == cliLoopbackHostname {
		return target, true
	}
	ip := net.ParseIP(host)
	if ip == nil || !(ip.Equal(net.ParseIP(cliLoopbackIPv4)) || ip.Equal(net.ParseIP(cliLoopbackIPv6))) {
		return nil, false
	}
	return target, true
}

func respondCliCode(ctx echo.Context, target string, code string) error {
	setNoStore(ctx)
	if target != cliTargetPaste {
		loopback, ok := getCliLoopback(target)
		if !ok {
			return respondError(ctx, http.StatusBadRequest, cliTargetRefusal)
		}
		q := loopback.Query()
		q.Set("code", code)
		loopback.RawQuery = q.Encode()
		return Redirect(ctx, http.StatusFound, loopback.String())
	}

	header := ctx.Response().Header()
	header.Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	header.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
	header.Set("X-Frame-Options", "DENY")
	header.Set("Referrer-Policy", "no-referrer")
	ctx.Response().WriteHeader(http.StatusOK)
	return cliCodeTemplate.Execute(ctx.Response(), TemplateCliCode{App: utils.Cfg().AppSlug, Code: code})
}

func respondOAuthError(ctx echo.Context, status int, code string, description string) error {
	setNoStore(ctx)
	return ctx.JSON(status, ResponseOAuthError{Error: code, ErrorDescription: description})
}

func setNoStore(ctx echo.Context) {
	ctx.Response().Header().Set("Cache-Control", "no-store")
	ctx.Response().Header().Set("Pragma", "no-cache")
}
