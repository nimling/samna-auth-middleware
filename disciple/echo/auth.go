package echo

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/smw"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

func getCookieApp(ctx echo.Context, name string) *shared.SamnaApp {
	cookie, err := ctx.Cookie(name)
	if err != nil || cookie.Value == "" {
		return nil
	}
	app, err := shared.GetApp(ctx.Request().Context(), cookie.Value)
	if err != nil {
		return nil
	}
	return app
}

func GetLogin(ctx echo.Context) error {
	slug := utils.Cfg().AppSlug
	var app *shared.SamnaApp
	if appSlug := ctx.QueryParam("app"); appSlug != "" && appSlug != slug {
		found, err := shared.GetApp(ctx.Request().Context(), appSlug)
		if err != nil {
			return respondError(ctx, http.StatusBadRequest, "unknown app")
		}
		app = found
		slug = found.Slug
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

	next := ctx.QueryParam("next")
	if next == "" {
		if referer, refErr := url.Parse(ctx.Request().Referer()); refErr == nil && strings.EqualFold(referer.Host, ctx.Request().Host) && referer.Path != ctx.Request().URL.Path {
			next = referer.RequestURI()
		}
	}
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") {
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
	authURL, err := client.AuthorizeURL(slug, redirectURI, state, challenge)
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
	if app != nil && cliTarget == "" {
		utils.WriteCookie(ctx, &http.Cookie{Name: client.LoginAppCookie, Value: app.Slug, Path: "/", MaxAge: 600})
	} else {
		utils.ClearCookie(ctx, client.LoginAppCookie)
	}

	return Redirect(ctx, http.StatusTemporaryRedirect, authURL)
}

func GetCallback(ctx echo.Context) error {
	code := ctx.QueryParam("code")
	if code == "" {
		return respondError(ctx, http.StatusBadRequest, "missing code")
	}

	app := getCookieApp(ctx, client.LoginAppCookie)
	startOver := utils.Cfg().PageRedirect
	if app != nil {
		startOver = app.GetPrimaryOrigin()
	}
	if startOver == "" {
		startOver = "/"
	}

	stateParam := ctx.QueryParam("state")
	stateNext := ""
	if dot := strings.LastIndex(stateParam, "."); dot >= 0 {
		if raw, decodeErr := base64.RawURLEncoding.DecodeString(stateParam[dot+1:]); decodeErr == nil && strings.HasPrefix(string(raw), "/") && !strings.HasPrefix(string(raw), "//") {
			stateNext = string(raw)
		}
	}
	resume := startOver
	if stateNext != "" {
		resume = strings.TrimSuffix(ctx.Request().URL.Path, "callback") + "login?next=" + url.QueryEscape(stateNext)
		if app != nil {
			resume += "&app=" + url.QueryEscape(app.Slug)
		}
	}
	stateCookie, stateErr := ctx.Cookie(client.OAuthStateCookie)
	if stateErr != nil || stateCookie.Value == "" || stateParam == "" || stateParam != stateCookie.Value {
		ctx.Logger().Warnf("GetCallback invalid state, clearing the flow cookies and sending the visitor back to start over")
		utils.ClearCookie(ctx, client.PKCEVerifierCookie)
		utils.ClearCookie(ctx, client.OAuthStateCookie)
		return Redirect(ctx, http.StatusFound, resume)
	}

	if cliCookie, cliErr := ctx.Cookie(client.CliTargetCookie); cliErr == nil && cliCookie.Value != "" {
		utils.ClearCookie(ctx, client.CliTargetCookie)
		utils.ClearCookie(ctx, client.OAuthStateCookie)
		utils.ClearCookie(ctx, client.LoginNextCookie)
		utils.ClearCookie(ctx, client.LoginAppCookie)
		return respondCliCode(ctx, cliCookie.Value, code)
	}

	verifierCookie, verifierErr := ctx.Cookie(client.PKCEVerifierCookie)
	if verifierErr != nil || verifierCookie.Value == "" {
		ctx.Logger().Warnf("GetCallback missing verifier, clearing the flow cookies and sending the visitor back to start over")
		utils.ClearCookie(ctx, client.PKCEVerifierCookie)
		utils.ClearCookie(ctx, client.OAuthStateCookie)
		return Redirect(ctx, http.StatusFound, resume)
	}

	slug := utils.Cfg().AppSlug
	secret := utils.Cfg().AppSecret
	redirectURI := utils.Cfg().RedirectURI
	tokenURL, err := client.TokenURLFor(slug)
	if err != nil {
		ctx.Logger().Errorf("GetCallback token url: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal Server Error")
	}

	tokens, err := client.PostAuthCode(ctx.Request().Context(), tokenURL, slug, secret, code, redirectURI, verifierCookie.Value)
	if err != nil {
		ctx.Logger().Warnf("GetCallback exchange refused, clearing the flow cookies and sending the visitor back to start over: %v", err)
		utils.ClearCookie(ctx, client.PKCEVerifierCookie)
		utils.ClearCookie(ctx, client.OAuthStateCookie)
		return Redirect(ctx, http.StatusFound, startOver)
	}

	utils.ClearCookie(ctx, client.PKCEVerifierCookie)
	utils.ClearCookie(ctx, client.OAuthStateCookie)

	utils.WriteCookie(ctx, &http.Cookie{Name: "sauth_access_token", Value: tokens.AccessToken, Path: "/", MaxAge: tokens.AccessExpiresIn})
	if tokens.RefreshToken != "" {
		utils.WriteCookie(ctx, &http.Cookie{Name: "sauth_refresh_token", Value: tokens.RefreshToken, Path: "/", MaxAge: tokens.RefreshExpiresIn})
	}
	smw.WriteCSRFCookie(ctx)

	pageRedirect := startOver
	if stateNext != "" {
		pageRedirect = stateNext
	}
	if nextCookie, nextErr := ctx.Cookie(client.LoginNextCookie); nextErr == nil && strings.HasPrefix(nextCookie.Value, "/") && !strings.HasPrefix(nextCookie.Value, "//") {
		pageRedirect = nextCookie.Value
	}
	utils.ClearCookie(ctx, client.LoginNextCookie)
	utils.ClearCookie(ctx, client.LoginAppCookie)
	if app != nil {
		utils.WriteCookie(ctx, &http.Cookie{Name: client.AppSlugCookie, Value: app.Slug, Path: "/", MaxAge: tokens.RefreshExpiresIn})
		if strings.HasPrefix(pageRedirect, "/") {
			pageRedirect = strings.TrimSuffix(app.GetPrimaryOrigin(), "/") + pageRedirect
		}
	} else {
		utils.ClearCookie(ctx, client.AppSlugCookie)
	}
	return Redirect(ctx, http.StatusFound, pageRedirect)
}

func GetLogout(ctx echo.Context) error {
	accessToken := ""
	if cookie, err := ctx.Cookie("sauth_access_token"); err == nil {
		accessToken = cookie.Value
	}

	slug, pageRedirect := utils.Cfg().AppSlug, utils.Cfg().PageRedirect
	if app := getCookieApp(ctx, client.AppSlugCookie); app != nil {
		slug, pageRedirect = app.Slug, app.GetPrimaryOrigin()
	}

	for _, name := range []string{"sauth_csrf_token", "sauth_refresh_token", "sauth_access_token", client.AppSlugCookie} {
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
