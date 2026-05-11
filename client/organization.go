package client

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
)

func GetOrganizationAudit(ctx context.Context, orgID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "organization", orgID.String(), "audit")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetOrganizationAssets(ctx context.Context, orgID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "organization", orgID.String(), "asset")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetOrganizations(ctx context.Context, rawQuery string) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "organization")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	if rawQuery != "" {
		u = u + "?" + rawQuery
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

func GetOrganizationGraph(ctx context.Context, startID *uuid.UUID, depth int, requiredAccess db.ClaimAccess) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "organization", "graph")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	u = appendGraphQuery(u, startID, depth, requiredAccess)
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
	if getDebugUserID() != nil {
		ac, err := db.Instance().MutationAccess(ctx, *getDebugUserID(), org.OwnerID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_WRITE)
		if err != nil {
			return db.Organization{}, err
		}
		if err := db.Instance().PostOrganization(ctx, ac, &org); err != nil {
			return db.Organization{}, err
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
	if getDebugUserID() != nil {
		ac, err := db.Instance().MutationAccess(ctx, *getDebugUserID(), orgID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_WRITE)
		if err != nil {
			return db.Organization{}, err
		}
		if err := db.Instance().PutOrganization(ctx, ac, orgID, &org); err != nil {
			return db.Organization{}, err
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
	if getDebugUserID() != nil {
		ac, err := db.Instance().MutationAccess(ctx, *getDebugUserID(), orgID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_WRITE)
		if err != nil {
			return db.Organization{}, err
		}
		out, err := db.Instance().DeleteOrganization(ctx, ac, orgID)
		if err != nil {
			return db.Organization{}, err
		}
		return *out, nil
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
