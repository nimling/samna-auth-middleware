package client

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
)

func GetLocationAssets(ctx context.Context, locationID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "location", locationID.String(), "assets")
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
	c := newClient()
	u, err := joinURL(c.apiURL, "location")
	if err != nil {
		return db.Location{}, fmt.Errorf("build url: %w", err)
	}
	return syncResponse[db.Location](ctx, c.client.Post(ctx, requestBody(loc, true), u))
}

func PutLocation(ctx context.Context, locationID uuid.UUID, loc db.Location) (db.Location, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "location", locationID.String())
	if err != nil {
		return db.Location{}, fmt.Errorf("build url: %w", err)
	}
	return syncResponse[db.Location](ctx, c.client.Put(ctx, requestBody(loc, false), u))
}

func DeleteLocation(ctx context.Context, locationID uuid.UUID) (db.Location, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "location", locationID.String())
	if err != nil {
		return db.Location{}, fmt.Errorf("build url: %w", err)
	}
	return syncResponse[db.Location](ctx, c.client.Delete(ctx, u))
}
