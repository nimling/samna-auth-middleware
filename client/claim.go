package client

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
)

func GetClaimGraph(ctx context.Context, startID *uuid.UUID, depth int, requiredAccess db.ClaimAccess) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", "graph")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	u = appendGraphQuery(u, startID, depth, requiredAccess)
	return c.client.Get(ctx, u)
}

func GetClaims(ctx context.Context, rawQuery string) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	if rawQuery != "" {
		u = u + "?" + rawQuery
	}
	return c.client.Get(ctx, u)
}

func GetClaim(ctx context.Context, claimID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetClaimAudit(ctx context.Context, claimID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "audit")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func PostClaim(ctx context.Context, claim db.Claim) (*db.Claim, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.Claim](c.client.Post(ctx, requestBody(claim, true), u))
	if err != nil {
		return nil, err
	}
	_, err = db.Instance().NewInsert().
		Model(&result).
		On("CONFLICT (id) DO UPDATE").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func PutClaim(ctx context.Context, claimID uuid.UUID, claim db.Claim) (*db.Claim, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.Claim](c.client.Put(ctx, requestBody(claim, false), u))
	if err != nil {
		return nil, err
	}
	_, err = db.Instance().NewInsert().
		Model(&result).
		On("CONFLICT (id) DO UPDATE").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func PatchClaim(ctx context.Context, claimID uuid.UUID, claim db.Claim) (*db.Claim, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.Claim](c.client.Patch(ctx, requestBody(claim, false), u))
	if err != nil {
		return nil, err
	}
	_, err = db.Instance().NewInsert().
		Model(&result).
		On("CONFLICT (id) DO UPDATE").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func DeleteClaim(ctx context.Context, claimID uuid.UUID) (*db.Claim, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.Claim](c.client.Delete(ctx, u))
	if err != nil {
		return nil, err
	}
	_, err = db.Instance().NewInsert().
		Model(&result).
		On("CONFLICT (id) DO UPDATE").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func PostClaimObject(ctx context.Context, claimID uuid.UUID, co db.ClaimObject) (*db.ClaimObject, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "object")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.ClaimObject](c.client.Post(ctx, requestBody(co, true), u))
	if err != nil {
		return nil, err
	}
	_, err = db.Instance().NewInsert().
		Model(&result).
		On("CONFLICT (id) DO UPDATE").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func DeleteClaimObject(ctx context.Context, claimID uuid.UUID, objectID uuid.UUID) (*db.ClaimObject, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "object", objectID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.ClaimObject](c.client.Delete(ctx, u))
	if err != nil {
		return nil, err
	}
	_, err = db.Instance().NewInsert().
		Model(&result).
		On("CONFLICT (id) DO UPDATE").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func PostUserClaim(ctx context.Context, claimID uuid.UUID, uc db.UserClaim) (*db.UserClaim, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "user")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.UserClaim](c.client.Post(ctx, requestBody(uc, true), u))
	if err != nil {
		return nil, err
	}
	_, err = db.Instance().NewInsert().
		Model(&result).
		On("CONFLICT (id) DO UPDATE").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func DeleteUserClaim(ctx context.Context, claimID uuid.UUID, userID uuid.UUID) (*db.UserClaim, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "user", userID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.UserClaim](c.client.Delete(ctx, u))
	if err != nil {
		return nil, err
	}
	_, err = db.Instance().NewInsert().
		Model(&result).
		On("CONFLICT (id) DO UPDATE").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Exec(ctx)
	if err != nil {
		return nil, err
	}
	return &result, nil
}
