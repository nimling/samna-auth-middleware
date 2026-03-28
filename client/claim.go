package client

import (
	"context"
	"fmt"
	"io"

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

func GetClaimSearch(ctx context.Context, claimID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "search")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func PostClaim(ctx context.Context, claim db.Claim) (db.Claim, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim")
	if err != nil {
		return db.Claim{}, fmt.Errorf("build url: %w", err)
	}
	return syncResponse[db.Claim](ctx, c.client.Post(ctx, requestBody(claim, true), u))
}

func PutClaim(ctx context.Context, claimID uuid.UUID, claim db.Claim) (db.Claim, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String())
	if err != nil {
		return db.Claim{}, fmt.Errorf("build url: %w", err)
	}
	return syncResponse[db.Claim](ctx, c.client.Put(ctx, requestBody(claim, false), u))
}

func DeleteClaim(ctx context.Context, claimID uuid.UUID) (db.Claim, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String())
	if err != nil {
		return db.Claim{}, fmt.Errorf("build url: %w", err)
	}
	return syncResponse[db.Claim](ctx, c.client.Delete(ctx, u))
}

func PostClaimObject(ctx context.Context, claimID uuid.UUID, co db.ClaimObject) (db.ClaimObject, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "object")
	if err != nil {
		return db.ClaimObject{}, fmt.Errorf("build url: %w", err)
	}
	return syncResponse[db.ClaimObject](ctx, c.client.Post(ctx, requestBody(co, true), u))
}

func DeleteClaimObject(ctx context.Context, claimID uuid.UUID, objectID uuid.UUID) (db.ClaimObject, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "object", objectID.String())
	if err != nil {
		return db.ClaimObject{}, fmt.Errorf("build url: %w", err)
	}
	return syncResponse[db.ClaimObject](ctx, c.client.Delete(ctx, u))
}

func PostUserClaim(ctx context.Context, claimID uuid.UUID, uc db.UserClaim) (db.UserClaim, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "user")
	if err != nil {
		return db.UserClaim{}, fmt.Errorf("build url: %w", err)
	}
	return syncResponse[db.UserClaim](ctx, c.client.Post(ctx, requestBody(uc, true), u))
}

func DeleteUserClaim(ctx context.Context, claimID uuid.UUID, userID uuid.UUID) (db.UserClaim, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "user", userID.String())
	if err != nil {
		return db.UserClaim{}, fmt.Errorf("build url: %w", err)
	}
	return syncResponse[db.UserClaim](ctx, c.client.Delete(ctx, u))
}
