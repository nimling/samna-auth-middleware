package client

import (
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func GetClaimGraph(ctx echo.Context) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", "graph")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetClaims(ctx echo.Context) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetClaim(ctx echo.Context, claimID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetClaimAudit(ctx echo.Context, claimID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "audit")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetClaimSearch(ctx echo.Context, claimID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "search")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func PostClaim(ctx echo.Context, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Post(ctx, body, u)
}

func PutClaim(ctx echo.Context, claimID uuid.UUID, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Put(ctx, body, u)
}

func DeleteClaim(ctx echo.Context, claimID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Delete(ctx, u)
}

func PostClaimObject(ctx echo.Context, claimID uuid.UUID, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "object")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Post(ctx, body, u)
}

func DeleteClaimObject(ctx echo.Context, claimID uuid.UUID, objectID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "object", objectID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Delete(ctx, u)
}

func PostClaimUser(ctx echo.Context, claimID uuid.UUID, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "user")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Post(ctx, body, u)
}

func DeleteClaimUser(ctx echo.Context, claimID uuid.UUID, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "claim", claimID.String(), "user", userID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Delete(ctx, u)
}
