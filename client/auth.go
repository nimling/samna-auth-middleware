package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type ResponseToken struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	AccessExpiresIn  int    `json:"access_expires_in"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
	TokenType        string `json:"token_type"`
}

type OpenIDConfiguration struct {
	Issuer        string `json:"issuer"`
	TokenEndpoint string `json:"token_endpoint"`
	JwksUri       string `json:"jwks_uri"`
}

func GetLogin(ctx context.Context, provider *string) (string, error) {
	endpoint := os.Getenv("SAUTH_ENDPOINT")

	var path string
	if provider != nil && len(*provider) > 0 {
		path = fmt.Sprintf("api/auth/p/%s/login", *provider)
	} else {
		path = "api/auth/login"
	}

	u, err := joinURL(endpoint, path)
	if err != nil {
		return "", fmt.Errorf("build url: %w", err)
	}

	parsed, err := url.Parse(u)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}

	q := parsed.Query()
	q.Set("client_slug", os.Getenv("SAUTH_APP_SLUG"))
	parsed.RawQuery = q.Encode()

	return parsed.String(), nil
}

func GetLogout(ctx context.Context) (int, error) {
	c := newClient()

	u, err := joinURL(c.apiURL, "logout")
	if err != nil {
		return 0, fmt.Errorf("build url: %w", err)
	}

	body, err := c.client.Get(ctx, u)
	if err != nil {
		return 0, err
	}
	body.Close()

	return 200, nil
}

func GetOpenIDConfiguration(ctx context.Context, configURL string) (*OpenIDConfiguration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, configURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpClient := &http.Client{Timeout: 3 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openid configuration request to %s returned status %d, check that the client slug exists", configURL, resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return nil, fmt.Errorf("openid configuration at %s returned %s instead of JSON: %s", configURL, contentType, string(body))
	}

	var config OpenIDConfiguration
	if err = json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return nil, fmt.Errorf("openid configuration at %s returned invalid JSON: %w", configURL, err)
	}

	return &config, nil
}

func postToken(ctx context.Context, tokenURL string, slug string, secret string, form url.Values) (*ResponseToken, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("SAuth-App-Slug", slug)
	req.Header.Set("SAuth-App-Secret", secret)

	httpClient := &http.Client{Timeout: 3 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("auth server returned status unauthorized: %w", ErrUnauthorized)
		}
		return nil, fmt.Errorf("auth server returned status %d", resp.StatusCode)
	}

	token, err := DecodeJSON[ResponseToken](resp.Body, nil)
	if err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &token, nil
}

func PostAuthCode(ctx context.Context, tokenURL string, slug string, secret string, code string, redirectURI string, codeVerifier string) (*ResponseToken, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", codeVerifier)
	return postToken(ctx, tokenURL, slug, secret, form)
}

func PostRefreshToken(ctx context.Context, tokenURL string, slug string, secret string, refreshToken string) (*ResponseToken, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	return postToken(ctx, tokenURL, slug, secret, form)
}
