package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/utils"
)

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

	var respBody ResponseToken
	if err = json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &respBody, nil
}

func exchangeAuthorizationCode(ctx echo.Context, authCode string, config AuthMiddlewareConfig) error {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", authCode)
	form.Set("redirect_uri", config.RedirectURI)

	tokenResp, err := performTokenRequest(ctx, form, config)
	if err != nil {
		return fmt.Errorf("perform token request: %w", err)
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
		return fmt.Errorf("openid configuration request to %s returned status %d, check that the client slug exists", config.OpenIDConfigURL.String(), resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("openid configuration at %s returned %s instead of JSON: %s", config.OpenIDConfigURL.String(), contentType, string(body))
	}

	var respBody openIDConfiguration
	if err = json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		return fmt.Errorf("openid configuration at %s returned invalid JSON: %w", config.OpenIDConfigURL.String(), err)
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

	fmt.Printf("\n\nGot OpenID Configuration: %+v\n", respBody)

	return nil
}
