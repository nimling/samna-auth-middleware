package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

func (d *DB) GetClient(ctx context.Context, clientID uuid.UUID) (*SamnaClient, error) {
	var client SamnaClient
	err := d.NewSelect().
		Model(&client).
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("client_id = ?", clientID).
		Where("sa_deleted_at IS NULL").
		Where("status = ?", "active").
		Where("expires_at IS NULL OR expires_at > ?", time.Now()).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("get client: %w", err)
	}
	return &client, nil
}

func (d *DB) GetClientClaims(ctx context.Context, clientID uuid.UUID) ([]UserClaim, error) {
	claims := make([]UserClaim, 0)
	err := d.NewSelect().
		Model(&claims).
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("user_id = ?", clientID).
		Where("sa_deleted_at IS NULL").
		Where("starts_at IS NULL OR starts_at <= CURRENT_TIMESTAMP").
		Where("ends_at IS NULL OR ends_at >= CURRENT_TIMESTAMP").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("get client claims: %w", err)
	}
	return claims, nil
}
