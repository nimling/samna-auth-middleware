package echo

import (
	"net/http"
	"os"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/middleware"
	"github.com/nimling/samna-auth-middleware/utils"
)

func GetLogin(ctx echo.Context) error {
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

	slug := os.Getenv("SAUTH_APP_SLUG")
	redirectURI := os.Getenv("SAUTH_REDIRECT_URI")
	authURL, err := client.AuthorizeURL(slug, redirectURI, state, client.ChallengeS256(verifier))
	if err != nil {
		ctx.Logger().Errorf("GetLogin authorize url: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal Server Error")
	}

	utils.WriteCookie(ctx, &http.Cookie{Name: client.PKCEVerifierCookie, Value: verifier, Path: "/", MaxAge: 600})
	utils.WriteCookie(ctx, &http.Cookie{Name: client.OAuthStateCookie, Value: state, Path: "/", MaxAge: 600})

	return Redirect(ctx, http.StatusTemporaryRedirect, authURL)
}

func GetCallback(ctx echo.Context) error {
	code := ctx.QueryParam("code")
	if code == "" {
		return respondError(ctx, http.StatusBadRequest, "missing code")
	}

	stateParam := ctx.QueryParam("state")
	stateCookie, stateErr := ctx.Cookie(client.OAuthStateCookie)
	if stateErr != nil || stateCookie.Value == "" || stateParam == "" || stateParam != stateCookie.Value {
		return respondError(ctx, http.StatusBadRequest, "invalid state")
	}

	verifierCookie, verifierErr := ctx.Cookie(client.PKCEVerifierCookie)
	if verifierErr != nil || verifierCookie.Value == "" {
		return respondError(ctx, http.StatusBadRequest, "missing verifier")
	}

	slug := os.Getenv("SAUTH_APP_SLUG")
	secret := os.Getenv("SAUTH_APP_SECRET")
	redirectURI := os.Getenv("SAUTH_REDIRECT_URI")
	tokenURL, err := client.TokenURLFor(slug)
	if err != nil {
		ctx.Logger().Errorf("GetCallback token url: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal Server Error")
	}

	tokens, err := client.PostAuthCode(ctx.Request().Context(), tokenURL, slug, secret, code, redirectURI, verifierCookie.Value)
	if err != nil {
		ctx.Logger().Errorf("GetCallback exchange: %v", err)
		return respondError(ctx, http.StatusUnauthorized, "Unauthorized")
	}

	utils.ClearCookie(ctx, client.PKCEVerifierCookie)
	utils.ClearCookie(ctx, client.OAuthStateCookie)

	utils.WriteCookie(ctx, &http.Cookie{Name: "sauth_access_token", Value: tokens.AccessToken, Path: "/", MaxAge: tokens.AccessExpiresIn})
	if tokens.RefreshToken != "" {
		utils.WriteCookie(ctx, &http.Cookie{Name: "sauth_refresh_token", Value: tokens.RefreshToken, Path: "/", MaxAge: tokens.RefreshExpiresIn})
	}
	middleware.WriteCSRFCookie(ctx)

	pageRedirect := os.Getenv("SAUTH_PAGE_REDIRECT")
	if pageRedirect == "" {
		pageRedirect = "/"
	}
	return Redirect(ctx, http.StatusFound, pageRedirect)
}

func GetLogout(ctx echo.Context) error {
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

	endSessionURL, err := client.EndSessionURL()
	if err != nil {
		ctx.Logger().Errorf("GetLogout end session url: %v", err)
		return ctx.NoContent(http.StatusOK)
	}
	return Redirect(ctx, http.StatusFound, endSessionURL)
}
