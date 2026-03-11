package client

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
)

func GetMe(ctx context.Context, meta RequestMetadata) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "me")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func PutMe(ctx context.Context, meta RequestMetadata, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "me")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Put(ctx, meta, body, u)
}

func GetMeClaims(ctx context.Context, meta RequestMetadata) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "me", "claim")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func PostMeAsset(ctx context.Context, meta RequestMetadata, contentType string, body io.Reader) (io.ReadCloser, error) {
	c := newClient()
	meAssetURL, err := joinURL(c.apiURL, "me", "asset")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, meAssetURL, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", meta.AccessToken))
	req.Header.Set("X-Request-ID", meta.RequestID)
	req.Header.Set("X-User-IP", meta.UserIP)
	req.Header.Set("SAuth-Client-Slug", c.client.clientSlug)
	req.Header.Set("SAuth-Client-Secret", c.client.clientSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return resp.Body, nil
}

func DeleteMeAsset(ctx context.Context, meta RequestMetadata, assetID uuid.UUID) error {
	c := newClient()
	u, err := joinURL(c.apiURL, "me", "asset", assetID.String())
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}

	_, err = c.client.Delete(ctx, meta, u)
	return err
}

func GetMeConnections(ctx context.Context, meta RequestMetadata) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "me", "user")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func GetMeModules(ctx context.Context, meta RequestMetadata) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "me", "modules")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func GetMeOrganizations(ctx context.Context, meta RequestMetadata) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "me", "organizations")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func GetMeLocations(ctx context.Context, meta RequestMetadata) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "me", "locations")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func GetMeClients(ctx context.Context, meta RequestMetadata) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "me", "clients")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func GetMeClient(ctx context.Context, meta RequestMetadata, clientID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "me", "clients", clientID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}
