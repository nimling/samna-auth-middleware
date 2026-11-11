package smw

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

func cookieValues(ctx echo.Context, name string) []string {
	values := make([]string, 0, 2)
	for _, cookie := range ctx.Request().Cookies() {
		if cookie.Name == name && cookie.Value != "" {
			values = append(values, cookie.Value)
		}
	}
	return values
}

func authenticateCookie(ctx echo.Context, config *AuthMiddlewareConfig, keyFunc jwt.Keyfunc) error {
	accessValues := cookieValues(ctx, accessTokenCookieKey)
	refreshValues := cookieValues(ctx, refreshTokenCookieKey)

	if len(accessValues) == 0 && len(refreshValues) == 0 {
		return ErrorCredentialMissing
	}

	if csrfErr := enforceCSRF(ctx); csrfErr != nil {
		return csrfErr
	}

	var accessErr error
	for _, value := range accessValues {
		claimUser, vldErr := ValidateToken(ctx.Request(), value, config, keyFunc)
		if vldErr == nil {
			ctx.Set(UserIDKey, claimUser.ID)
			ctx.Set(AccessTokenKey, value)
			return nil
		}
		accessErr = vldErr
	}
	if accessErr != nil && !errors.Is(accessErr, ErrorTokenExpired) && len(refreshValues) == 0 {
		ctx.Logger().Errorf("Failed to validate the cookie access_token: %v", accessErr)
		utils.ClearCookie(ctx, accessTokenCookieKey)
		utils.ClearCookie(ctx, refreshTokenCookieKey)
		utils.ClearCookie(ctx, csrfTokenCookieKey)
		return ErrorCredentialMissing
	}

	tokens, err := refreshTokens(ctx, *config)
	if err != nil {
		ctx.Logger().Warnf("Failed to refresh tokens: %v", err)
		if errors.Is(err, client.ErrUnauthorized) {
			utils.ClearCookie(ctx, accessTokenCookieKey)
			utils.ClearCookie(ctx, refreshTokenCookieKey)
			utils.ClearCookie(ctx, csrfTokenCookieKey)
			return ErrorCredentialMissing
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
	values := cookieValues(ctx, csrfTokenCookieKey)
	if len(values) == 0 {
		return fmt.Errorf("csrf cookie missing: %w", ErrorForbidden)
	}
	header := ctx.Request().Header.Get("SAuth-CSRF-Token")
	if header == "" {
		return fmt.Errorf("csrf token mismatch: %w", ErrorForbidden)
	}
	for _, value := range values {
		if header == value {
			return nil
		}
	}
	return fmt.Errorf("csrf token mismatch: %w", ErrorForbidden)
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
	values := cookieValues(ctx, refreshTokenCookieKey)
	if len(values) == 0 {
		return nil, fmt.Errorf("refresh token from cookie is empty: %w", ErrorUnauthorized)
	}

	var tokenResp *client.ResponseToken
	var err error
	for _, value := range values {
		tokenResp, err = client.PostRefreshToken(ctx.Request().Context(), config.TokenURL.String(), config.ClientSlug, config.ClientSecret, value)
		if err == nil {
			break
		}
	}
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

func InitiateMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		if origin := ctx.Request().Header.Get(echo.HeaderOrigin); origin != "" {
			if initiate, err := shared.GetOriginInitiateApp(ctx.Request().Context(), origin); err == nil {
				ctx.Set(utils.CookieScopeKey, initiate.GetCookieScope())
			}
		}
		return next(ctx)
	}
}

func GetOriginAllowed(origins []string) func(string) (bool, error) {
	return func(origin string) (bool, error) {
		if slices.Contains(origins, origin) {
			return true, nil
		}
		_, err := shared.GetOriginInitiateApp(context.Background(), origin)
		return err == nil, nil
	}
}
