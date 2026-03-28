package client

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
)

func GetUserAssets(ctx context.Context, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "assets")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetUsers(ctx context.Context) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetUser(ctx context.Context, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetUserClaims(ctx context.Context, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "claim")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetUserConnections(ctx context.Context, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "user")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func PostUser(ctx context.Context, userID uuid.UUID, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Post(ctx, body, u)
}

func PutUser(ctx context.Context, userID uuid.UUID, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Put(ctx, body, u)
}

func DeleteUser(ctx context.Context, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Delete(ctx, u)
}

func GetUserClients(ctx context.Context, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "clients")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetUserClient(ctx context.Context, userID uuid.UUID, clientID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "clients", clientID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func PutUserClient(ctx context.Context, userID uuid.UUID, clientID uuid.UUID, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "clients", clientID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Put(ctx, body, u)
}
