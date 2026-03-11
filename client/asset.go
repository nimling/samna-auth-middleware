package client

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
)

func GetAssets(ctx context.Context, meta RequestMetadata) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "asset")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func GetAsset(ctx context.Context, meta RequestMetadata, assetID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "asset", assetID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func GetAssetData(ctx context.Context, meta RequestMetadata, assetID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "asset", assetID.String(), "data")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func PostAsset(ctx context.Context, meta RequestMetadata, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "asset")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Post(ctx, meta, body, u)
}

func PostAssetUpload(ctx context.Context, meta RequestMetadata, assetID uuid.UUID, contentType string, body io.Reader) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "asset", assetID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, body)
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

func PutAsset(ctx context.Context, meta RequestMetadata, assetID uuid.UUID, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "asset", assetID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Put(ctx, meta, body, u)
}

func DeleteAsset(ctx context.Context, meta RequestMetadata, assetID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "asset", assetID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Delete(ctx, meta, u)
}
