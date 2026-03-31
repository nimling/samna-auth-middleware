package client

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/utils"
)

func PostRelation(ctx context.Context, relation db.UserRelation) (db.UserRelation, error) {
	if utils.GetDebugUserID() != nil {
		relation.ID = uuid.New()
		relation.CreatedAt = time.Now()
		relation.UpdatedAt = time.Now()
		_, err := db.Instance().NewInsert().Model(&relation).Exec(ctx)
		if err != nil {
			return relation, fmt.Errorf("debug insert relation: %w", err)
		}
		return relation, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "relation")
	if err != nil {
		return db.UserRelation{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.UserRelation](c.client.Post(ctx, requestBody(relation, true), u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func PutRelation(ctx context.Context, relationID uuid.UUID, relation db.UserRelation) (db.UserRelation, error) {
	if utils.GetDebugUserID() != nil {
		relation.ID = relationID
		relation.UpdatedAt = time.Now()
		_, err := db.Instance().NewUpdate().Model(&relation).Where("id = ?", relationID).OmitZero().Exec(ctx)
		if err != nil {
			return relation, fmt.Errorf("debug update relation: %w", err)
		}
		return relation, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "relation", relationID.String())
	if err != nil {
		return db.UserRelation{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.UserRelation](c.client.Put(ctx, requestBody(relation, false), u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}

func DeleteRelation(ctx context.Context, relationID uuid.UUID) (db.UserRelation, error) {
	if utils.GetDebugUserID() != nil {
		var result db.UserRelation
		_, err := db.Instance().NewUpdate().Model(&result).Set("sa_deleted_at = CURRENT_TIMESTAMP").Where("id = ?", relationID).Where("sa_deleted_at IS NULL").Returning("*").Exec(ctx)
		if err != nil {
			return result, fmt.Errorf("debug delete relation: %w", err)
		}
		return result, nil
	}
	c := newClient()
	u, err := joinURL(c.apiURL, "relation", relationID.String())
	if err != nil {
		return db.UserRelation{}, fmt.Errorf("build url: %w", err)
	}
	result, err := DecodeJSON[db.UserRelation](c.client.Delete(ctx, u))
	if err != nil {
		return result, err
	}
	_, err = db.Instance().NewInsert().Model(&result).On("CONFLICT (id) DO UPDATE").Set("sa_updated_at = EXCLUDED.sa_updated_at").Set("sa_deleted_at = EXCLUDED.sa_deleted_at").Exec(ctx)
	if err != nil {
		return result, err
	}
	return result, nil
}
