package client

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
)

func GetUsers(ctx context.Context, meta RequestMetadata) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func GetUser(ctx context.Context, meta RequestMetadata, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func GetUserClaims(ctx context.Context, meta RequestMetadata, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "claim")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func GetUserConnections(ctx context.Context, meta RequestMetadata, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "user")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func PostUser(ctx context.Context, meta RequestMetadata, userID uuid.UUID, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Post(ctx, meta, body, u)
}

func PutUser(ctx context.Context, meta RequestMetadata, userID uuid.UUID, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Put(ctx, meta, body, u)
}

func DeleteUser(ctx context.Context, meta RequestMetadata, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Delete(ctx, meta, u)
}

func GetUserClients(ctx context.Context, meta RequestMetadata, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "clients")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func GetUserClient(ctx context.Context, meta RequestMetadata, userID uuid.UUID, clientID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "clients", clientID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, meta, u)
}

func PutUserClient(ctx context.Context, meta RequestMetadata, userID uuid.UUID, clientID uuid.UUID, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "clients", clientID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Put(ctx, meta, body, u)
}
