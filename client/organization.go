package client

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
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

func PostOrganization(ctx context.Context, org db.Organization) (db.Organization, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "organization")
	if err != nil {
		return db.Organization{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.Organization](c.client.Post(ctx, requestBody(org, true), u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func PutOrganization(ctx context.Context, orgID uuid.UUID, org db.Organization) (db.Organization, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "organization", orgID.String())
	if err != nil {
		return db.Organization{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.Organization](c.client.Put(ctx, requestBody(org, false), u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func DeleteOrganization(ctx context.Context, orgID uuid.UUID) (db.Organization, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "organization", orgID.String())
	if err != nil {
		return db.Organization{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.Organization](c.client.Delete(ctx, u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}
