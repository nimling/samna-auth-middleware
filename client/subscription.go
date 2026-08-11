package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nimling/samna-auth-middleware/utils"
)

// PostSubscriptionFeed registers an address against a feed under the app's own
// client credentials, so a reader with no session can subscribe. The credentials
// never reach the browser; the caller is the process holding them.
func PostSubscriptionFeed(ctx context.Context, slug string, clientID string, clientSecret string, email string, feed string) error {
	endpoint, err := joinURL(utils.Cfg().Endpoint, fmt.Sprintf("api/app/%s/subscription/client", slug))
	if err != nil {
		return fmt.Errorf("build subscription url: %w", err)
	}

	parsed, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("parse subscription url: %w", err)
	}
	query := parsed.Query()
	query.Set("email", email)
	parsed.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), strings.NewReader(feed))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/rss+xml")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(clientID, clientSecret)

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("subscription request returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
