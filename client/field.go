package client

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
)

func GetObjectFields(ctx context.Context) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "field")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetObjectField(ctx context.Context, fieldID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "field", fieldID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func PostObjectField(ctx context.Context, field db.ObjectField) (db.ObjectField, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "field")
	if err != nil {
		return db.ObjectField{}, fmt.Errorf("build url: %w", err)
	}
	return DecodeJSON[db.ObjectField](c.client.Post(ctx, requestBody(field, true), u))
}

func PutObjectField(ctx context.Context, fieldID uuid.UUID, field db.ObjectField) (db.ObjectField, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "field", fieldID.String())
	if err != nil {
		return db.ObjectField{}, fmt.Errorf("build url: %w", err)
	}
	return DecodeJSON[db.ObjectField](c.client.Put(ctx, requestBody(field, false), u))
}

func PatchObjectField(ctx context.Context, fieldID uuid.UUID, field db.ObjectField) (db.ObjectField, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "field", fieldID.String())
	if err != nil {
		return db.ObjectField{}, fmt.Errorf("build url: %w", err)
	}
	return DecodeJSON[db.ObjectField](c.client.Patch(ctx, requestBody(field, false), u))
}

func DeleteObjectField(ctx context.Context, fieldID uuid.UUID) (db.ObjectField, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "field", fieldID.String())
	if err != nil {
		return db.ObjectField{}, fmt.Errorf("build url: %w", err)
	}
	return DecodeJSON[db.ObjectField](c.client.Delete(ctx, u))
}

func GetUserFields(ctx context.Context, fieldID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "field", fieldID.String(), "value")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func PostUserField(ctx context.Context, fieldID uuid.UUID, field db.UserField) (db.UserField, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "field", fieldID.String(), "value")
	if err != nil {
		return db.UserField{}, fmt.Errorf("build url: %w", err)
	}
	return DecodeJSON[db.UserField](c.client.Post(ctx, requestBody(field, true), u))
}

func PutUserField(ctx context.Context, fieldID uuid.UUID, valueID uuid.UUID, field db.UserField) (db.UserField, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "field", fieldID.String(), "value", valueID.String())
	if err != nil {
		return db.UserField{}, fmt.Errorf("build url: %w", err)
	}
	return DecodeJSON[db.UserField](c.client.Put(ctx, requestBody(field, false), u))
}

func PatchUserField(ctx context.Context, fieldID uuid.UUID, valueID uuid.UUID, field db.UserField) (db.UserField, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "field", fieldID.String(), "value", valueID.String())
	if err != nil {
		return db.UserField{}, fmt.Errorf("build url: %w", err)
	}
	return DecodeJSON[db.UserField](c.client.Patch(ctx, requestBody(field, false), u))
}

func DeleteUserField(ctx context.Context, fieldID uuid.UUID, valueID uuid.UUID) (db.UserField, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "field", fieldID.String(), "value", valueID.String())
	if err != nil {
		return db.UserField{}, fmt.Errorf("build url: %w", err)
	}
	return DecodeJSON[db.UserField](c.client.Delete(ctx, u))
}
