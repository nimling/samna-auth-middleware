package client

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
)

func GetLocationAudit(ctx context.Context, locationID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "location", locationID.String(), "audit")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetLocationAssets(ctx context.Context, locationID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "location", locationID.String(), "asset")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetLocations(ctx context.Context) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "location")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetLocation(ctx context.Context, locationID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "location", locationID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetLocationClaims(ctx context.Context, locationID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "location", locationID.String(), "claim")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func PostLocation(ctx context.Context, loc db.Location) (db.Location, error) {
	if getDebugUserID() != nil {
		if err := db.Instance().PostLocation(ctx, &loc); err != nil {
			return loc, fmt.Errorf("debug insert location: %w", err)
		}
		return loc, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "location")
	if err != nil {
		return db.Location{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.Location](c.client.Post(ctx, requestBody(loc, true), u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func PutLocation(ctx context.Context, locationID uuid.UUID, loc db.Location) (db.Location, error) {
	if getDebugUserID() != nil {
		if err := db.Instance().PutLocation(ctx, locationID, &loc); err != nil {
			return loc, fmt.Errorf("debug update location: %w", err)
		}
		return loc, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "location", locationID.String())
	if err != nil {
		return db.Location{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.Location](c.client.Put(ctx, requestBody(loc, false), u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func DeleteLocation(ctx context.Context, locationID uuid.UUID) (db.Location, error) {
	if getDebugUserID() != nil {
		var result db.Location
		_, err := db.Instance().NewUpdate().Model(&result).Set("sa_deleted_at = NOW()").Where("id = ?", locationID).Where("sa_deleted_at IS NULL").Returning("*").Exec(ctx)
		if err != nil {
			return result, fmt.Errorf("debug delete location: %w", err)
		}
		return result, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "location", locationID.String())
	if err != nil {
		return db.Location{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.Location](c.client.Delete(ctx, u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}
