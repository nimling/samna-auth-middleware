package client

import (
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func GetAssets(ctx echo.Context) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "asset")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetAsset(ctx echo.Context, assetID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "asset", assetID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetAssetData(ctx echo.Context, assetID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "asset", assetID.String(), "data")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func PostAsset(ctx echo.Context, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "asset")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Post(ctx, body, u)
}

func PostAssetUpload(ctx echo.Context, assetID uuid.UUID, contentType string, body io.Reader) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "asset", assetID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx.Request().Context(), http.MethodPost, u, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	accessToken, _ := ctx.Get("access_token").(string)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	req.Header.Set("X-Request-ID", ctx.Response().Header().Get(echo.HeaderXRequestID))
	req.Header.Set("X-User-IP", ctx.RealIP())
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

func PutAsset(ctx echo.Context, assetID uuid.UUID, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "asset", assetID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Put(ctx, body, u)
}

func DeleteAsset(ctx echo.Context, assetID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "asset", assetID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Delete(ctx, u)
}
