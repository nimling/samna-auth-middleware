package client

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"github.com/nimling/samna-auth-middleware/v2/utils"
	"net/url"
)

const (
	PKCEVerifierCookie = "sauth_pkce_verifier"
	OAuthStateCookie   = "sauth_oauth_state"
	LoginNextCookie    = "sauth_login_next"
	CliTargetCookie    = "sauth_cli_target"
)

func GenerateVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func ChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func AuthorizeURL(slug string, redirectURI string, state string, challenge string) (string, error) {
	endpoint := utils.Cfg().Endpoint
	joined, err := joinURL(endpoint, fmt.Sprintf("api/app/%s/oauth/authorize", slug))
	if err != nil {
		return "", fmt.Errorf("build authorize url: %w", err)
	}
	parsed, err := url.Parse(joined)
	if err != nil {
		return "", fmt.Errorf("parse authorize url: %w", err)
	}
	q := parsed.Query()
	q.Set("response_type", "code")
	q.Set("client_id", slug)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}

func TokenURLFor(slug string) (string, error) {
	endpoint := utils.Cfg().Endpoint
	return joinURL(endpoint, fmt.Sprintf("api/app/%s/oauth/token", slug))
}

func EndSessionURL() (string, error) {
	endpoint := utils.Cfg().Endpoint
	slug := utils.Cfg().AppSlug
	joined, err := joinURL(endpoint, fmt.Sprintf("api/app/%s/oauth/end_session", slug))
	if err != nil {
		return "", fmt.Errorf("build end session url: %w", err)
	}
	parsed, err := url.Parse(joined)
	if err != nil {
		return "", fmt.Errorf("parse end session url: %w", err)
	}
	pageRedirect := utils.Cfg().PageRedirect
	if pageRedirect != "" {
		q := parsed.Query()
		q.Set("post_logout_redirect_uri", pageRedirect)
		parsed.RawQuery = q.Encode()
	}
	return parsed.String(), nil
}

func RevokeURLFor(slug string) (string, error) {
	endpoint := utils.Cfg().Endpoint
	return joinURL(endpoint, fmt.Sprintf("api/app/%s/oauth/revoke", slug))
}
