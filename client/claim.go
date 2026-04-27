package client

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
)

func GetClaimGraph(ctx context.Context) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", "graph")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetClaims(ctx context.Context) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
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

func PostClaim(ctx context.Context, claim db.Claim) (db.Claim, error) {
	if getDebugUserID() != nil {
		appID, err := resolveAppID(ctx)
		if err != nil {
			return claim, err
		}
		claim.ID = uuid.New()
		claim.AppID = appID
		claim.CreatedAt = time.Now()
		claim.UpdatedAt = time.Now()
		_, err = db.Instance().NewInsert().Model(&claim).Exec(ctx)
		if err != nil {
			return claim, fmt.Errorf("debug insert claim: %w", err)
		}
		return claim, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "claim")
	if err != nil {
		return db.Claim{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.Claim](c.client.Post(ctx, requestBody(claim, true), u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func PutClaim(ctx context.Context, claimID uuid.UUID, claim db.Claim) (db.Claim, error) {
	if getDebugUserID() != nil {
		claim.ID = claimID
		claim.UpdatedAt = time.Now()
		_, err := db.Instance().NewUpdate().Model(&claim).Where("id = ?", claimID).OmitZero().Exec(ctx)
		if err != nil {
			return claim, fmt.Errorf("debug update claim: %w", err)
		}
		return claim, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String())
	if err != nil {
		return db.Claim{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.Claim](c.client.Put(ctx, requestBody(claim, false), u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func DeleteClaim(ctx context.Context, claimID uuid.UUID) (db.Claim, error) {
	if getDebugUserID() != nil {
		var claim db.Claim
		_, err := db.Instance().NewUpdate().Model(&claim).Set("sa_deleted_at = NOW()").Where("id = ?", claimID).Where("sa_deleted_at IS NULL").Returning("*").Exec(ctx)
		if err != nil {
			return claim, fmt.Errorf("debug delete claim: %w", err)
		}
		return claim, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String())
	if err != nil {
		return db.Claim{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.Claim](c.client.Delete(ctx, u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func PostClaimObject(ctx context.Context, claimID uuid.UUID, co db.ClaimObject) (db.ClaimObject, error) {
	if getDebugUserID() != nil {
		appID, err := resolveAppID(ctx)
		if err != nil {
			return co, err
		}
		co.ID = uuid.New()
		co.AppID = appID
		co.ClaimID = claimID
		co.CreatedAt = time.Now()
		co.UpdatedAt = time.Now()
		_, err = db.Instance().NewInsert().Model(&co).Exec(ctx)
		if err != nil {
			return co, fmt.Errorf("debug insert claim object: %w", err)
		}
		return co, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "object")
	if err != nil {
		return db.ClaimObject{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.ClaimObject](c.client.Post(ctx, requestBody(co, true), u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func DeleteClaimObject(ctx context.Context, claimID uuid.UUID, objectID uuid.UUID) (db.ClaimObject, error) {
	if getDebugUserID() != nil {
		var co db.ClaimObject
		_, err := db.Instance().NewUpdate().Model(&co).Set("sa_deleted_at = NOW()").Where("claim_id = ?", claimID).Where("object_id = ?", objectID).Where("sa_deleted_at IS NULL").Returning("*").Exec(ctx)
		if err != nil {
			return co, fmt.Errorf("debug delete claim object: %w", err)
		}
		return co, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "object", objectID.String())
	if err != nil {
		return db.ClaimObject{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.ClaimObject](c.client.Delete(ctx, u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func PostUserClaim(ctx context.Context, claimID uuid.UUID, uc db.UserClaim) (db.UserClaim, error) {
	if getDebugUserID() != nil {
		appID, err := resolveAppID(ctx)
		if err != nil {
			return uc, err
		}
		uc.ID = uuid.New()
		uc.AppID = appID
		uc.ClaimID = claimID
		uc.CreatedAt = time.Now()
		uc.UpdatedAt = time.Now()
		_, err = db.Instance().NewInsert().Model(&uc).Exec(ctx)
		if err != nil {
			return uc, fmt.Errorf("debug insert user claim: %w", err)
		}
		return uc, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "user")
	if err != nil {
		return db.UserClaim{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.UserClaim](c.client.Post(ctx, requestBody(uc, true), u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func DeleteUserClaim(ctx context.Context, claimID uuid.UUID, userID uuid.UUID) (db.UserClaim, error) {
	if getDebugUserID() != nil {
		var uc db.UserClaim
		_, err := db.Instance().NewUpdate().Model(&uc).Set("sa_deleted_at = NOW()").Where("claim_id = ?", claimID).Where("user_id = ?", userID).Where("sa_deleted_at IS NULL").Returning("*").Exec(ctx)
		if err != nil {
			return uc, fmt.Errorf("debug delete user claim: %w", err)
		}
		return uc, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "user", userID.String())
	if err != nil {
		return db.UserClaim{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.UserClaim](c.client.Delete(ctx, u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}
