package client

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
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

func PostLocation(ctx context.Context, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "location")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Post(ctx, body, u)
}

func PutLocation(ctx context.Context, locationID uuid.UUID, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "location", locationID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Put(ctx, body, u)
}

func DeleteLocation(ctx context.Context, locationID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "location", locationID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Delete(ctx, u)
}
