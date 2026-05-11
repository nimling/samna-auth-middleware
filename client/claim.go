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

func PostClaim(ctx context.Context, claim db.Claim) (db.Claim, error) {
	if getDebugUserID() != nil {
		out, err := db.Instance().PostClaim(ctx, claim)
		if err != nil {
			return db.Claim{}, err
		}
		return *out, nil
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
		out, err := db.Instance().PutClaim(ctx, claimID, claim)
		if err != nil {
			return db.Claim{}, err
		}
		return *out, nil
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
		ac, err := db.Instance().MutationAccess(ctx, *getDebugUserID(), claimID, db.OBJECT_TYPE_CLAIM, db.ACCESS_OWNER)
		if err != nil {
			return db.Claim{}, err
		}
		if ac == nil || ac.UserClaimID == nil {
			return db.Claim{}, fmt.Errorf("forbidden")
		}
		if err := db.Instance().DeleteClaim(ctx, claimID, *ac.UserClaimID); err != nil {
			return db.Claim{}, err
		}
		out, err := db.Instance().GetClaim(ctx, *getDebugUserID(), claimID, db.ACCESS_NONE)
		if err != nil || out == nil {
			return db.Claim{ID: claimID}, nil
		}
		return *out, nil
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
		out, err := db.Instance().PostClaimObject(ctx, claimID, co)
		if err != nil {
			return db.ClaimObject{}, err
		}
		return *out, nil
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
		ac, err := db.Instance().MutationAccess(ctx, *getDebugUserID(), claimID, db.OBJECT_TYPE_CLAIM, db.ACCESS_OWNER)
		if err != nil {
			return db.ClaimObject{}, err
		}
		if ac == nil || ac.UserClaimID == nil {
			return db.ClaimObject{}, fmt.Errorf("forbidden")
		}
		if err := db.Instance().DeleteClaimObject(ctx, claimID, objectID, *ac.UserClaimID); err != nil {
			return db.ClaimObject{}, err
		}
		return db.ClaimObject{ClaimID: claimID, ObjectID: &objectID}, nil
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
		out, err := db.Instance().PostClaimUser(ctx, claimID, uc)
		if err != nil {
			return db.UserClaim{}, err
		}
		return *out, nil
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
		ac, err := db.Instance().MutationAccess(ctx, *getDebugUserID(), claimID, db.OBJECT_TYPE_CLAIM, db.ACCESS_OWNER)
		if err != nil {
			return db.UserClaim{}, err
		}
		if ac == nil || ac.UserClaimID == nil {
			return db.UserClaim{}, fmt.Errorf("forbidden")
		}
		if err := db.Instance().DeleteClaimUser(ctx, claimID, userID, *ac.UserClaimID); err != nil {
			return db.UserClaim{}, err
		}
		return db.UserClaim{ClaimID: claimID, UserID: userID}, nil
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
