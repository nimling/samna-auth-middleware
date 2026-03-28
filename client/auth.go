package client

import (
	"context"
	"fmt"
	"net/url"
	"os"
)

func GetLogin(ctx context.Context, provider *string) (string, error) {
	endpoint := os.Getenv("SAUTH_ENDPOINT")

	var path string
	if provider != nil && len(*provider) > 0 {
		path = fmt.Sprintf("api/auth/p/%s/login", *provider)
	} else {
		path = "login"
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
	q.Set("client_slug", os.Getenv("SAUTH_CLIENT_SLUG"))
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
