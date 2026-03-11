package client

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
)

func GetClaimGraph(ctx context.Context, meta RequestMetadata) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", "graph")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func GetClaim(ctx context.Context, meta RequestMetadata, claimID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func PostClaim(ctx context.Context, meta RequestMetadata, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Post(ctx, meta, body, u)
}

func PutClaim(ctx context.Context, meta RequestMetadata, claimID uuid.UUID, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Put(ctx, meta, body, u)
}

func DeleteClaim(ctx context.Context, meta RequestMetadata, claimID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Delete(ctx, meta, u)
}

func PostClaimObject(ctx context.Context, meta RequestMetadata, claimID uuid.UUID, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "object")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Post(ctx, meta, body, u)
}

func DeleteClaimObject(ctx context.Context, meta RequestMetadata, claimObjectID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", "object", claimObjectID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Delete(ctx, meta, u)
}

func PostClaimUser(ctx context.Context, meta RequestMetadata, claimID uuid.UUID, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "user")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Post(ctx, meta, body, u)
}

func DeleteClaimUser(ctx context.Context, meta RequestMetadata, userClaimID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", "user", userClaimID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Delete(ctx, meta, u)
}
