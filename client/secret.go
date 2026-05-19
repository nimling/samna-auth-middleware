package client

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
)

func GetSecrets(ctx context.Context, rawQuery string) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "secret")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	if rawQuery != "" {
		u = u + "?" + rawQuery
	}
	return c.client.Get(ctx, u)
}

func GetSecret(ctx context.Context, secretID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "secret", secretID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func PostSecret(ctx context.Context, body db.BodySecret) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "secret")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Post(ctx, body, u)
}

func PutSecret(ctx context.Context, secretID uuid.UUID, body db.BodySecret) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "secret", secretID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Put(ctx, body, u)
}

func PatchSecret(ctx context.Context, secretID uuid.UUID, body db.BodySecret) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "secret", secretID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Patch(ctx, body, u)
}

func DeleteSecret(ctx context.Context, secretID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "secret", secretID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Delete(ctx, u)
}
