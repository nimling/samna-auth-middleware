package client

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
)

func GetLocations(ctx context.Context, meta RequestMetadata) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "location")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func GetLocation(ctx context.Context, meta RequestMetadata, locationID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "location", locationID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func GetLocationClaims(ctx context.Context, meta RequestMetadata, locationID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "location", locationID.String(), "claim")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func PostLocation(ctx context.Context, meta RequestMetadata, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "location")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Post(ctx, meta, body, u)
}

func PutLocation(ctx context.Context, meta RequestMetadata, locationID uuid.UUID, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "location", locationID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Put(ctx, meta, body, u)
}

func DeleteLocation(ctx context.Context, meta RequestMetadata, locationID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "location", locationID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Delete(ctx, meta, u)
}
