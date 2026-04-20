package client

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
)

func GetClients(ctx context.Context) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "client")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetSamnaClient(ctx context.Context, clientID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "client", clientID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func PostSamnaClient(ctx context.Context, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "client")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Post(ctx, body, u)
}

func PutSamnaClient(ctx context.Context, clientID uuid.UUID, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "client", clientID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Put(ctx, body, u)
}

func DeleteSamnaClient(ctx context.Context, clientID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "client", clientID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Delete(ctx, u)
}

func PostSamnaClientRotate(ctx context.Context, clientID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "client", clientID.String(), "rotate")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Post(ctx, nil, u)
}

func PostSamnaClientClaim(ctx context.Context, clientID uuid.UUID, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "client", clientID.String(), "claim")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Post(ctx, body, u)
}

func DeleteSamnaClientClaim(ctx context.Context, clientID uuid.UUID, claimID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "client", clientID.String(), "claim", claimID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Delete(ctx, u)
}
