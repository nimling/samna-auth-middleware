package middleware

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/utils"
)

func generateCSRFToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func writeCSRFCookie(ctx echo.Context) {
	token, err := generateCSRFToken()
	if err != nil {
		ctx.Logger().Errorf("Failed to generate CSRF token: %v", err)
		return
	}
	utils.WritePublicCookie(ctx, &http.Cookie{
		Name:   csrfTokenCookieKey,
		Value:  token,
		Path:   "/",
		MaxAge: 86400,
	})
}

func refreshTokens(ctx echo.Context, config AuthMiddlewareConfig) (*client.ResponseToken, error) {
	cookie, err := ctx.Cookie(refreshTokenCookieKey)
	if err != nil {
		return nil, fmt.Errorf("obtain refresh_token from cookie: %w: %w", err, ErrorUnauthorized)
	}

	if cookie.Value == "" {
		return nil, fmt.Errorf("refresh token from cookie is empty: %w", ErrorUnauthorized)
	}

	tokenResp, err := client.PostRefreshToken(ctx.Request().Context(), config.TokenURL.String(), config.ClientSlug, config.ClientSecret, cookie.Value)
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
			MaxAge: tokenResp.RefreshExpiresIn,
		})
	}

	writeCSRFCookie(ctx)

	return tokenResp, nil
}

func exchangeAuthorizationCode(ctx echo.Context, authCode string, config AuthMiddlewareConfig) error {
	tokenResp, err := client.PostAuthCode(ctx.Request().Context(), config.TokenURL.String(), config.ClientSlug, config.ClientSecret, authCode, config.RedirectURI)
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
		MaxAge: tokenResp.RefreshExpiresIn,
	})

	writeCSRFCookie(ctx)

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

	log.Printf("samna_auth_middleware: oidc issuer=%s token=%s jwks=%s", oidc.Issuer, oidc.TokenEndpoint, oidc.JwksUri)

	return nil
}
