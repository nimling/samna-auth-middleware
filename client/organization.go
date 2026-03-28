package client

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
)

func GetOrganizationAssets(ctx context.Context, orgID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "organization", orgID.String(), "assets")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetOrganizations(ctx context.Context) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "organization")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetOrganization(ctx context.Context, orgID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "organization", orgID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetOrganizationGraph(ctx context.Context) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "organization", "graph")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetOrganizationClaims(ctx context.Context, orgID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "organization", orgID.String(), "claim")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func PostOrganization(ctx context.Context, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "organization")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Post(ctx, body, u)
}

func PutOrganization(ctx context.Context, orgID uuid.UUID, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "organization", orgID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Put(ctx, body, u)
}

func DeleteOrganization(ctx context.Context, orgID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "organization", orgID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Delete(ctx, u)
}
