package client

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/utils"
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
	if utils.GetDebugUserID() != nil {
		org.ID = uuid.New()
		org.CreatedAt = time.Now()
		org.UpdatedAt = time.Now()
		_, err := db.Instance().NewInsert().Model(&org).Exec(ctx)
		if err != nil {
			return org, fmt.Errorf("debug insert organization: %w", err)
		}
		return org, nil
	}
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
	if utils.GetDebugUserID() != nil {
		org.ID = orgID
		org.UpdatedAt = time.Now()
		_, err := db.Instance().NewUpdate().Model(&org).Where("id = ?", orgID).OmitZero().Exec(ctx)
		if err != nil {
			return org, fmt.Errorf("debug update organization: %w", err)
		}
		return org, nil
	}
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
	if utils.GetDebugUserID() != nil {
		var result db.Organization
		_, err := db.Instance().NewUpdate().Model(&result).Set("sa_deleted_at = NOW()").Where("id = ?", orgID).Where("sa_deleted_at IS NULL").Returning("*").Exec(ctx)
		if err != nil {
			return result, fmt.Errorf("debug delete organization: %w", err)
		}
		return result, nil
	}
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
