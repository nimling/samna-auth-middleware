package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/utils"
)

func refreshTokens(ctx echo.Context, config AuthMiddlewareConfig) (*client.ResponseToken, error) {
	cookie, err := ctx.Cookie(refreshTokenCookieKey)
	if err != nil {
		return nil, fmt.Errorf("obtain refresh_token from cookie: %w: %w", err, ErrorUnauthorized)
	}

	if cookie.Value == "" {
		return nil, fmt.Errorf("refresh token from cookie is empty: %w", ErrorUnauthorized)
	}

	form := url.Values{}
	form.Set("grant_type", "sauth_refresh_token")
	form.Set("refresh_token", cookie.Value)

	tokenResp, err := client.PostToken(ctx.Request().Context(), config.TokenURL.String(), config.ClientSlug, config.ClientSecret, form)
	if err != nil {
		return nil, fmt.Errorf("refresh tokens: %w", err)
	}

	utils.WriteCookie(ctx, &http.Cookie{
		Name:   accessTokenCookieKey,
		Value:  tokenResp.AccessToken,
		Path:   "/",
		MaxAge: tokenResp.AccessExpiresIn,
	})

	if tokenResp.RefreshToken != "" {
		utils.WriteCookie(ctx, &http.Cookie{
			Name:   refreshTokenCookieKey,
			Value:  tokenResp.RefreshToken,
			Path:   "/",
			MaxAge: 30 * 24 * 3600,
		})
	}

	return tokenResp, nil
}

func exchangeAuthorizationCode(ctx echo.Context, authCode string, config AuthMiddlewareConfig) error {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", authCode)
	form.Set("redirect_uri", config.RedirectURI)

	tokenResp, err := client.PostToken(ctx.Request().Context(), config.TokenURL.String(), config.ClientSlug, config.ClientSecret, form)
	if err != nil {
		return fmt.Errorf("exchange authorization code: %w", err)
	}

	utils.WriteCookie(ctx, &http.Cookie{
		Name:   accessTokenCookieKey,
		Value:  tokenResp.AccessToken,
		Path:   "/",
		MaxAge: tokenResp.AccessExpiresIn,
	})

	utils.WriteCookie(ctx, &http.Cookie{
		Name:   refreshTokenCookieKey,
		Value:  tokenResp.RefreshToken,
		Path:   "/",
		MaxAge: 30 * 24 * 3600,
	})

	return nil
}

func getOpenIDConfiguration(ctx context.Context, config *AuthMiddlewareConfig) error {
	oidc, err := client.GetOpenIDConfiguration(ctx, config.OpenIDConfigURL.String())
	if err != nil {
		return err
	}

	jwksUrl, err := url.Parse(oidc.JwksUri)
	if err != nil {
		return fmt.Errorf("parse jwks uri: %w", err)
	}

	tokenUrl, err := url.Parse(oidc.TokenEndpoint)
	if err != nil {
		return fmt.Errorf("parse token endpoint: %w", err)
	}

	config.ExpectedTokenIssuer = oidc.Issuer
	config.JWKSURL = *jwksUrl
	config.TokenURL = *tokenUrl

	fmt.Printf("\n\nGot OpenID Configuration: %+v\n", oidc)

	return nil
}
