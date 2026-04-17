package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

func (d *DB) GetClientByKey(ctx context.Context, clientKey uuid.UUID) (*SamnaClient, error) {
	var client SamnaClient
	err := d.NewSelect().
		Model(&client).
		Where("client_key = ?", clientKey).
		Where("sa_deleted_at IS NULL").
		Where("status = ?", "active").
		Where("expires_at IS NULL OR expires_at > ?", time.Now()).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("get client by key: %w", err)
	}
	return &client, nil
}

func (d *DB) GetClientClaims(ctx context.Context, clientID uuid.UUID) ([]UserClaim, error) {
	var claims []UserClaim
	err := d.NewSelect().
		Model(&claims).
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
