package client

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
)

func GetMe(ctx context.Context) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "me")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func PutMe(ctx context.Context, user db.SamnaUser) (db.SamnaUser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "me")
	if err != nil {
		return db.SamnaUser{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.SamnaUser](c.client.Put(ctx, requestBody(user, false), u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func GetMeClaims(ctx context.Context) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "me", "claim")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func PostMeAsset(ctx context.Context, contentType string, body io.Reader) (io.ReadCloser, error) {
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
	req.Header.Set("X-Request-ID", generateRequestID())
	req.Header.Set("X-Client-IP", localIP())
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

func DeleteMeAsset(ctx context.Context, assetID uuid.UUID) error {
	c := newClient()
	u, err := joinURL(c.apiURL, "me", "asset", assetID.String())
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}

	_, err = c.client.Delete(ctx, u)
	return err
}

func GetMeConnections(ctx context.Context) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "me", "user")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetMeModules(ctx context.Context) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "me", "modules")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetMeOrganizations(ctx context.Context) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "me", "organizations")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetMeLocations(ctx context.Context) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "me", "locations")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetMeClients(ctx context.Context) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "me", "clients")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetMeClient(ctx context.Context, clientID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "me", "clients", clientID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}
