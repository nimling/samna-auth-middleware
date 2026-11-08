package smw

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

func authenticateCookie(ctx echo.Context, config *AuthMiddlewareConfig, keyFunc jwt.Keyfunc) error {
	accessCookie, accessErr := ctx.Cookie(accessTokenCookieKey)
	refreshCookie, refreshErr := ctx.Cookie(refreshTokenCookieKey)

	accessPresent := accessErr == nil && accessCookie.Value != ""
	refreshPresent := refreshErr == nil && refreshCookie.Value != ""

	if !accessPresent && !refreshPresent {
		return ErrorCredentialMissing
	}

	if csrfErr := enforceCSRF(ctx); csrfErr != nil {
		return csrfErr
	}

	if accessPresent {
		claimUser, vldErr := ValidateToken(ctx.Request(), accessCookie.Value, config, keyFunc)
		if vldErr == nil {
			ctx.Set(UserIDKey, claimUser.ID)
			ctx.Set(AccessTokenKey, accessCookie.Value)
			return nil
		}
		if !errors.Is(vldErr, ErrorTokenExpired) {
			ctx.Logger().Errorf("Failed to validate the cookie access_token: %v", vldErr)
			return fmt.Errorf("validate cookie token: %w: %w", vldErr, ErrorUnauthorized)
		}
	}

	tokens, err := refreshTokens(ctx, *config)
	if err != nil {
		ctx.Logger().Warnf("Failed to refresh tokens: %v", err)
		if errors.Is(err, client.ErrUnauthorized) {
			utils.ClearCookie(ctx, accessTokenCookieKey)
			utils.ClearCookie(ctx, refreshTokenCookieKey)
			utils.ClearCookie(ctx, csrfTokenCookieKey)
			return fmt.Errorf("refresh rejected: %w: %w", err, ErrorUnauthorized)
		}
		return fmt.Errorf("refresh failed: %w: %w", err, ErrorUnauthorized)
	}

	claims, err := claimsFromTokenUnsafe(tokens.AccessToken)
	if err != nil {
		ctx.Logger().Errorf("Failed to parse the refreshed token claims: %v", err)
		return fmt.Errorf("parse refreshed token claims: %w", err)
	}

	ctx.Set(UserIDKey, claims.ID)
	ctx.Set(AccessTokenKey, tokens.AccessToken)
	return nil
}

func enforceCSRF(ctx echo.Context) error {
	method := ctx.Request().Method
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return nil
	}
	cookie, err := ctx.Cookie(csrfTokenCookieKey)
	if err != nil || cookie.Value == "" {
		return fmt.Errorf("csrf cookie missing: %w", ErrorForbidden)
	}
	header := ctx.Request().Header.Get("SAuth-CSRF-Token")
	if header == "" || header != cookie.Value {
		return fmt.Errorf("csrf token mismatch: %w", ErrorForbidden)
	}
	return nil
}

func generateCSRFToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func WriteCSRFCookie(ctx echo.Context) {
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

	if held, heldErr := ctx.Cookie(csrfTokenCookieKey); heldErr == nil && held.Value != "" {
		utils.WritePublicCookie(ctx, &http.Cookie{Name: csrfTokenCookieKey, Value: held.Value, Path: "/", MaxAge: 86400})
	} else {
		WriteCSRFCookie(ctx)
	}

	return tokenResp, nil
}
