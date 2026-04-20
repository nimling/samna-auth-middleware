package client

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/utils"
)

func GetUserAudit(ctx context.Context, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "audit")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetUserAssets(ctx context.Context, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "asset")
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

func PostUser(ctx context.Context, userID uuid.UUID, user db.SamnaUser) (db.SamnaUser, error) {
	if utils.GetDebugUserID() != nil {
		user.ID = uuid.New()
		user.CreatedAt = time.Now()
		user.UpdatedAt = time.Now()
		_, err := db.Instance().NewInsert().Model(&user).Exec(ctx)
		if err != nil {
			return user, fmt.Errorf("debug insert user: %w", err)
		}
		return user, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String())
	if err != nil {
		return db.SamnaUser{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.SamnaUser](c.client.Post(ctx, requestBody(user, true), u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func PutUser(ctx context.Context, userID uuid.UUID, user db.SamnaUser) (db.SamnaUser, error) {
	if utils.GetDebugUserID() != nil {
		user.UpdatedAt = time.Now()
		_, err := db.Instance().NewUpdate().Model(&user).Where("user_id = ?", userID).OmitZero().Exec(ctx)
		if err != nil {
			return user, fmt.Errorf("debug update user: %w", err)
		}
		return user, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String())
	if err != nil {
		return db.SamnaUser{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.SamnaUser](c.client.Put(ctx, requestBody(user, false), u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func DeleteUser(ctx context.Context, userID uuid.UUID) (db.SamnaUser, error) {
	if utils.GetDebugUserID() != nil {
		var result db.SamnaUser
		_, err := db.Instance().NewUpdate().Model(&result).Set("sa_deleted_at = NOW()").Where("user_id = ?", userID).Where("sa_deleted_at IS NULL").Returning("*").Exec(ctx)
		if err != nil {
			return result, fmt.Errorf("debug delete user: %w", err)
		}
		return result, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String())
	if err != nil {
		return db.SamnaUser{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.SamnaUser](c.client.Delete(ctx, u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func GetUserApps(ctx context.Context, userID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "apps")
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func GetUserApp(ctx context.Context, userID uuid.UUID, appID uuid.UUID) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "apps", appID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Get(ctx, u)
}

func PutUserApp(ctx context.Context, userID uuid.UUID, appID uuid.UUID, body any) (io.ReadCloser, error) {
	c := newClient()
	u, err := joinURL(c.apiURL, "user", userID.String(), "apps", appID.String())
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	return c.client.Put(ctx, body, u)
}
