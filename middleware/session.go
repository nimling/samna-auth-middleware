package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

const (
	accessTokenCookieKey  = "sauth_access_token"
	refreshTokenCookieKey = "sauth_refresh_token"
)

// refreshTokens refreshes the access and refresh token
func refreshTokens(ctx echo.Context, config AuthMiddlewareConfig) (*ResponseToken, error) {
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

	tokenResp, err := performTokenRequest(ctx, form, config)
	if err != nil {
		return nil, fmt.Errorf("perform token request: %w", err)
	}

	// Write cookies
	writeCookie(ctx, config, &http.Cookie{
		Name:   accessTokenCookieKey,
		Value:  tokenResp.AccessToken,
		Path:   "/",
		MaxAge: int(tokenResp.ExpiresIn),
	})

	if tokenResp.RefreshToken != "" { // if auth server returned new refresh_token
		writeCookie(ctx, config, &http.Cookie{
			Name:   refreshTokenCookieKey,
			Value:  tokenResp.RefreshToken,
			Path:   "/",            // TODO:: Set this to another specific path?
			MaxAge: 30 * 24 * 3600, // TODO:: idk, from OAuth standard, refresh_token exp in tokenResp is not required. Maybe we want to add this
		})
	}

	return tokenResp, nil
}

// writeCookie writes a user client cookie
func writeCookie(ctx echo.Context, config AuthMiddlewareConfig, cookie *http.Cookie) {

	cookie = &http.Cookie{
		Name:     cookie.Name,
		Value:    cookie.Value,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Path:     cookie.Path,
		MaxAge:   cookie.MaxAge,
	}

	if config.CookieDomain != "" {
		cookie.Domain = config.CookieDomain

		if strings.Contains(config.CookieDomain, "localhost") {
			cookie.Secure = false
		}
	}

	ctx.SetCookie(cookie)
}

func clearCookie(ctx echo.Context, name string) {

	cookie := &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}

	cookieDomain, ok := os.LookupEnv("SAMNA_AUTH_COOKIE_DOMAIN")
	if ok {
		if strings.Contains(cookieDomain, "localhost") {
			cookie.Secure = false
		}
		cookie.Domain = cookieDomain
	}

	ctx.SetCookie(cookie)
}

// performTokenRequest performs a token refresh or initial request
func performTokenRequest(ctx echo.Context, params url.Values, config AuthMiddlewareConfig) (*ResponseToken, error) {
	encodedData := params.Encode()

	targetURL := config.TokenURL.String()

	req, err := http.NewRequestWithContext(ctx.Request().Context(), http.MethodPost, targetURL, strings.NewReader(encodedData))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	req.SetBasicAuth(config.ClientSlug, config.ClientSecret)

	client := &http.Client{Timeout: 3 * time.Second}

	// Perform request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("auth server returned status unauthorized: %w", ErrorUnauthorized)
		}

		return nil, fmt.Errorf("auth server returned status %d", resp.StatusCode)
	}

	// Parse response body
	var respBody ResponseToken
	if err = json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &respBody, nil
}

// exchangeAuthorizationCode redirects the request to the auth middleware
func exchangeAuthorizationCode(ctx echo.Context, authCode string, config AuthMiddlewareConfig) error {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", authCode)
	form.Set("redirect_uri", config.RedirectURI)

	tokenResp, err := performTokenRequest(ctx, form, config)
	if err != nil {
		return fmt.Errorf("perform token request: %w", err)
	}

	// Write cookies
	writeCookie(ctx, config, &http.Cookie{
		Name:   accessTokenCookieKey,
		Value:  tokenResp.AccessToken,
		Path:   "/",
		MaxAge: int(tokenResp.ExpiresIn),
	})

	writeCookie(ctx, config, &http.Cookie{
		Name:   refreshTokenCookieKey,
		Value:  tokenResp.RefreshToken,
		Path:   "/",            // TODO:: Set this to another specific path?
		MaxAge: 30 * 24 * 3600, // TODO:: idk, from OAuth standard, refresh_token exp in tokenResp is not required. Maybe we want to add this
	})

	return nil
}

type openIDConfiguration struct {
	Issuer        string `json:"issuer"`
	TokenEndpoint string `json:"token_endpoint"`
	JwksUri       string `json:"jwks_uri"`
}

func getOpenIDConfiguration(ctx context.Context, config *AuthMiddlewareConfig) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, config.OpenIDConfigURL.String(), nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth server returned status %d", resp.StatusCode)
	}

	var respBody openIDConfiguration
	if err = json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	jwksUrl, err := url.Parse(respBody.JwksUri)
	if err != nil {
		return fmt.Errorf("parse jwks uri: %w", err)
	}

	tokenUrl, err := url.Parse(respBody.TokenEndpoint)
	if err != nil {
		return fmt.Errorf("parse token endpoint: %w", err)
	}

	config.ExpectedTokenIssuer = respBody.Issuer
	config.JWKSURL = *jwksUrl
	config.TokenURL = *tokenUrl

	return nil
}
