package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/utils"
)

func GetAssets(ctx context.Context) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "asset")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetAsset(ctx context.Context, assetID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "asset", assetID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetAssetData(ctx context.Context, assetID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "asset", assetID.String(), "data")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func PostAsset(ctx context.Context, asset db.Asset) (db.Asset, error) {
	if utils.GetDebugUserID() != nil {
		asset.ID = uuid.New()
		asset.CreatedAt = time.Now()
		asset.UpdatedAt = time.Now()
		_, err := db.Instance().NewInsert().Model(&asset).Exec(ctx)
		if err != nil {
			return asset, fmt.Errorf("debug insert asset: %w", err)
		}
		return asset, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "asset")
	if err != nil {
		return db.Asset{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.Asset](c.client.Post(ctx, requestBody(asset, true), u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func PostAssetUpload(ctx context.Context, assetID uuid.UUID, contentType string, body io.Reader) (io.ReadCloser, error) {
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

func PutAsset(ctx context.Context, assetID uuid.UUID, asset db.Asset) (db.Asset, error) {
	if utils.GetDebugUserID() != nil {
		asset.ID = assetID
		asset.UpdatedAt = time.Now()
		_, err := db.Instance().NewUpdate().Model(&asset).Where("id = ?", assetID).OmitZero().Exec(ctx)
		if err != nil {
			return asset, fmt.Errorf("debug update asset: %w", err)
		}
		return asset, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "asset", assetID.String())
	if err != nil {
		return db.Asset{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.Asset](c.client.Put(ctx, requestBody(asset, false), u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func DeleteAsset(ctx context.Context, assetID uuid.UUID) (db.Asset, error) {
	if utils.GetDebugUserID() != nil {
		var result db.Asset
		_, err := db.Instance().NewUpdate().Model(&result).Set("sa_deleted_at = NOW()").Where("id = ?", assetID).Where("sa_deleted_at IS NULL").Returning("*").Exec(ctx)
		if err != nil {
			return result, fmt.Errorf("debug delete asset: %w", err)
		}
		return result, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "asset", assetID.String())
	if err != nil {
		return db.Asset{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.Asset](c.client.Delete(ctx, u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}
