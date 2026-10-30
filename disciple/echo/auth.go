package echo

import (
	_ "embed"
	"encoding/base64"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/smw"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

const (
	loginPromptLogin       = "login"
	loginRetryWindow       = 60
	oauthErrInvalidRequest = "invalid_request"
	oauthErrInvalidClient  = "invalid_client"
	oauthErrServerError    = "server_error"
	loginErrNoCode         = "The sign in came back without an authorization code."
	loginErrState          = "The sign in state does not match this browser."
	loginErrVerifier       = "This browser holds no verifier for the sign in."
	loginErrExchange       = "The authorization code was refused."
	loginErrProphet        = "The sign in could not be completed."
	loginErrRefused        = "The sign in was refused."
	loginErrRetryExhausted = "It failed twice within a minute, so it stops here instead of trying again."
)

//go:embed login/error.html
var loginErrorSource string

var loginErrorTemplate = template.Must(template.New("error").Parse(loginErrorSource))

type TemplateLoginError struct {
	App         string
	Error       string
	Description string
	Logout      string
}

func GetLogin(ctx echo.Context) error {
	slug := utils.Cfg().AppSlug
	var initiate *shared.SamnaApp
	if appSlug := ctx.Param("app"); appSlug != "" {
		found, err := shared.GetInitiateApp(ctx.Request().Context(), appSlug)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "unknown app")
		}
		initiate = found
		slug = found.Slug
		ctx.Set(utils.CookieScopeKey, found.GetCookieScope())
	}

	verifier, err := client.GenerateVerifier()
	if err != nil {
		ctx.Logger().Errorf("GetLogin verifier: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal Server Error")
	}
	state, err := client.GenerateVerifier()
	if err != nil {
		ctx.Logger().Errorf("GetLogin state: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal Server Error")
	}

	landing, origins := getReturnScope(initiate)
	next := ctx.QueryParam("next")
	if next == "" {
		if referer, refErr := url.Parse(ctx.Request().Referer()); refErr == nil && strings.EqualFold(referer.Host, ctx.Request().Host) && referer.Path != ctx.Request().URL.Path {
			next = referer.RequestURI()
		}
	}
	if getReturnTo(next, landing, origins) != "" {
		state += "." + base64.RawURLEncoding.EncodeToString([]byte(next))
		utils.WriteCookie(ctx, &http.Cookie{Name: client.LoginNextCookie, Value: next, Path: "/", MaxAge: 600})
	}

	challenge := client.ChallengeS256(verifier)
	cliTarget := ""
	if cliChallenge := ctx.QueryParam("cli_challenge"); cliChallenge != "" {
		target, ok := getCliTarget(cliChallenge, ctx.QueryParam("cli_redirect"), ctx.QueryParam("cli_state"))
		if !ok {
			return respondError(ctx, http.StatusBadRequest, cliTargetRefusal)
		}
		challenge = cliChallenge
		cliTarget = target
	}

	redirectURI := utils.Cfg().RedirectURI
	if initiate != nil {
		redirectURI = initiate.GetCallbackURI()
	}
	params := url.Values{}
	for _, key := range []string{"prompt", "error", "error_description"} {
		if value := ctx.QueryParam(key); value != "" {
			params.Set(key, value)
		}
	}
	authURL, err := client.AuthorizeURL(slug, redirectURI, state, challenge, params)
	if err != nil {
		ctx.Logger().Errorf("GetLogin authorize url: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal Server Error")
	}

	if cliTarget != "" {
		utils.ClearCookie(ctx, client.PKCEVerifierCookie)
		utils.WriteCookie(ctx, &http.Cookie{Name: client.CliTargetCookie, Value: cliTarget, Path: "/", MaxAge: 600})
	} else {
		utils.ClearCookie(ctx, client.CliTargetCookie)
		utils.WriteCookie(ctx, &http.Cookie{Name: client.PKCEVerifierCookie, Value: verifier, Path: "/", MaxAge: 600})
	}
	utils.WriteCookie(ctx, &http.Cookie{Name: client.OAuthStateCookie, Value: state, Path: "/", MaxAge: 600})

	return Redirect(ctx, http.StatusTemporaryRedirect, authURL)
}

func GetCallback(ctx echo.Context) error {
	redirectURI := utils.Cfg().RedirectURI
	startOver := utils.Cfg().PageRedirect
	var initiate *shared.SamnaApp
	if appSlug := ctx.QueryParam("app"); appSlug != "" {
		found, err := shared.GetInitiateApp(ctx.Request().Context(), appSlug)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "unknown app")
		}
		initiate = found
		redirectURI = found.GetCallbackURI()
		startOver = found.GetPrimaryOrigin()
		ctx.Set(utils.CookieScopeKey, found.GetCookieScope())
	}
	if startOver == "" {
		startOver = "/"
	}

	authPath := strings.TrimSuffix(ctx.Request().URL.Path, "callback")
	if initiate != nil {
		authPath += url.PathEscape(initiate.Slug) + "/"
	}

	landing, origins := getReturnScope(initiate)
	stateParam := ctx.QueryParam("state")
	stateNext := ""
	if dot := strings.LastIndex(stateParam, "."); dot >= 0 {
		if raw, decodeErr := base64.RawURLEncoding.DecodeString(stateParam[dot+1:]); decodeErr == nil && getReturnTo(string(raw), landing, origins) != "" {
			stateNext = string(raw)
		}
	}

	if refused := ctx.QueryParam("error"); refused != "" {
		description := ctx.QueryParam("error_description")
		if description == "" {
			description = loginErrRefused
		}
		return respondLoginError(ctx, authPath, stateNext, refused, description, errors.New("the authorize request was answered with an error"))
	}
	code := ctx.QueryParam("code")
	if code == "" {
		return respondLoginError(ctx, authPath, stateNext, oauthErrInvalidRequest, loginErrNoCode, errors.New("no code in the callback"))
	}

	stateCookie, stateErr := ctx.Cookie(client.OAuthStateCookie)
	if stateErr != nil || stateCookie.Value == "" || stateParam == "" || stateParam != stateCookie.Value {
		return respondLoginError(ctx, authPath, stateNext, oauthErrInvalidRequest, loginErrState, errors.New("state cookie missing or not equal to the state parameter"))
	}

	if cliCookie, cliErr := ctx.Cookie(client.CliTargetCookie); cliErr == nil && cliCookie.Value != "" {
		utils.ClearCookie(ctx, client.CliTargetCookie)
		utils.ClearCookie(ctx, client.OAuthStateCookie)
		utils.ClearCookie(ctx, client.LoginNextCookie)
		return respondCliCode(ctx, cliCookie.Value, code)
	}

	verifierCookie, verifierErr := ctx.Cookie(client.PKCEVerifierCookie)
	if verifierErr != nil || verifierCookie.Value == "" {
		return respondLoginError(ctx, authPath, stateNext, oauthErrInvalidRequest, loginErrVerifier, errors.New("verifier cookie missing"))
	}

	rootSlug := utils.Cfg().AppSlug
	tokenURL, err := client.TokenURLFor(rootSlug)
	if err != nil {
		ctx.Logger().Errorf("GetCallback token url: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal Server Error")
	}

	tokens, err := client.PostAuthCode(ctx.Request().Context(), tokenURL, rootSlug, utils.Cfg().AppSecret, code, redirectURI, verifierCookie.Value)
	if err != nil {
		var tokenErr *client.TokenError
		if errors.As(err, &tokenErr) && tokenErr.Status < http.StatusInternalServerError && tokenErr.Code != "" && tokenErr.Code != oauthErrInvalidClient {
			description := tokenErr.Description
			if description == "" {
				description = loginErrExchange
			}
			return respondLoginError(ctx, authPath, stateNext, tokenErr.Code, description, err)
		}
		return respondLoginError(ctx, authPath, stateNext, oauthErrServerError, loginErrProphet, err)
	}

	utils.ClearCookie(ctx, client.PKCEVerifierCookie)
	utils.ClearCookie(ctx, client.OAuthStateCookie)
	utils.ClearCookie(ctx, client.LoginRetryCookie)

	utils.WriteCookie(ctx, &http.Cookie{Name: "sauth_access_token", Value: tokens.AccessToken, Path: "/", MaxAge: tokens.AccessExpiresIn})
	if tokens.RefreshToken != "" {
		utils.WriteCookie(ctx, &http.Cookie{Name: "sauth_refresh_token", Value: tokens.RefreshToken, Path: "/", MaxAge: tokens.RefreshExpiresIn})
	}
	smw.WriteCSRFCookie(ctx)

	pageRedirect := startOver
	if target := getReturnTo(stateNext, landing, origins); target != "" {
		pageRedirect = target
	}
	if nextCookie, nextErr := ctx.Cookie(client.LoginNextCookie); nextErr == nil {
		if target := getReturnTo(nextCookie.Value, landing, origins); target != "" {
			pageRedirect = target
		}
	}
	utils.ClearCookie(ctx, client.LoginNextCookie)
	return Redirect(ctx, http.StatusFound, pageRedirect)
}

func respondLoginError(ctx echo.Context, authPath string, next string, code string, description string, cause error) error {
	ctx.Logger().Warnf("GetCallback %s: %s: %v", code, description, cause)
	for _, name := range []string{client.PKCEVerifierCookie, client.OAuthStateCookie, client.CliTargetCookie, client.LoginNextCookie, "sauth_csrf_token", "sauth_refresh_token", "sauth_access_token"} {
		utils.ClearCookie(ctx, name)
	}
	setNoStore(ctx)

	retries := 0
	if cookie, err := ctx.Cookie(client.LoginRetryCookie); err == nil {
		retries, _ = strconv.Atoi(cookie.Value)
	}
	if retries > 0 {
		header := ctx.Response().Header()
		header.Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
		header.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "no-referrer")
		ctx.Response().WriteHeader(http.StatusBadRequest)
		return loginErrorTemplate.Execute(ctx.Response(), TemplateLoginError{App: utils.Cfg().AppSlug, Error: code, Description: description + " " + loginErrRetryExhausted, Logout: authPath + "logout"})
	}
	utils.WriteCookie(ctx, &http.Cookie{Name: client.LoginRetryCookie, Value: strconv.Itoa(retries + 1), Path: "/", MaxAge: loginRetryWindow})

	params := url.Values{}
	params.Set("prompt", loginPromptLogin)
	params.Set("error", code)
	params.Set("error_description", description)
	if next != "" {
		params.Set("next", next)
	}
	return Redirect(ctx, http.StatusTemporaryRedirect, authPath+"login?"+params.Encode())
}

func GetLogout(ctx echo.Context) error {
	slug, pageRedirect := utils.Cfg().AppSlug, utils.Cfg().PageRedirect
	if appSlug := ctx.Param("app"); appSlug != "" {
		initiate, err := shared.GetInitiateApp(ctx.Request().Context(), appSlug)
		if err != nil {
			return respondError(ctx, http.StatusNotFound, "unknown app")
		}
		slug, pageRedirect = initiate.Slug, initiate.GetPrimaryOrigin()
		ctx.Set(utils.CookieScopeKey, initiate.GetCookieScope())
	}

	accessToken := ""
	if cookie, err := ctx.Cookie("sauth_access_token"); err == nil {
		accessToken = cookie.Value
	}

	for _, name := range []string{"sauth_csrf_token", "sauth_refresh_token", "sauth_access_token"} {
		utils.ClearCookie(ctx, name)
	}

	if accessToken != "" {
		statusCode, err := client.GetLogout(ctx.Request().Context(), accessToken)
		if err != nil {
			ctx.Logger().Errorf("GetLogout: %v", err)
		} else if statusCode >= http.StatusBadRequest {
			ctx.Logger().Errorf("GetLogout: auth server returned %d", statusCode)
		}
	}

	endSessionURL, err := client.EndSessionURL(slug, pageRedirect)
	if err != nil {
		ctx.Logger().Errorf("GetLogout end session url: %v", err)
		return ctx.NoContent(http.StatusOK)
	}
	return Redirect(ctx, http.StatusFound, endSessionURL)
}

func getReturnScope(initiate *shared.SamnaApp) (string, []string) {
	if initiate != nil {
		return initiate.GetPrimaryOrigin(), initiate.GetOrigins()
	}
	landing := utils.Cfg().PageRedirect
	if parsed, err := url.Parse(landing); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return landing, []string{parsed.Scheme + "://" + parsed.Host}
	}
	return landing, nil
}

func getReturnTo(raw string, landing string, origins []string) string {
	if raw == "" || strings.ContainsAny(raw, "\\\r\n\t") {
		return ""
	}
	if strings.HasPrefix(raw, "/") {
		if strings.HasPrefix(raw, "//") {
			return ""
		}
		if base, err := url.Parse(landing); err == nil && base.Scheme != "" && base.Host != "" {
			return base.Scheme + "://" + base.Host + raw
		}
		return raw
	}
	target, err := url.Parse(raw)
	if err != nil || (target.Scheme != "https" && target.Scheme != "http") || target.Host == "" || target.User != nil {
		return ""
	}
	if slices.Contains(origins, target.Scheme+"://"+target.Host) {
		return target.String()
	}
	return ""
}
