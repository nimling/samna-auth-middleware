package client

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
)

func GetRelations(ctx context.Context, rawQuery string) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "relation")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	if rawQuery != "" {
		u = u + "?" + rawQuery
	}
	return c.client.Get(ctx, u)
}

func GetRelation(ctx context.Context, relationID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "relation", relationID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func PostRelation(ctx context.Context, relation db.UserRelation) (*db.UserRelation, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "relation")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.UserRelation](c.client.Post(ctx, requestBody(relation, true), u))
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

func PutRelation(ctx context.Context, relationID uuid.UUID, relation db.UserRelation) (*db.UserRelation, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "relation", relationID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.UserRelation](c.client.Put(ctx, requestBody(relation, false), u))
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

func DeleteRelation(ctx context.Context, relationID uuid.UUID) (*db.UserRelation, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "relation", relationID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.UserRelation](c.client.Delete(ctx, u))
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
