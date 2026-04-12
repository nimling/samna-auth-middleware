package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
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

func (d *DB) GetClientClaims(ctx context.Context, clientID uuid.UUID) ([]ClientClaim, error) {
	var claims []ClientClaim
	err := d.NewSelect().
		Model(&claims).
		Where("client_id = ?", clientID).
		Where("sa_deleted_at IS NULL").
		Where("? IS NULL OR ? > ?", bun.Ident("starts_at"), bun.Ident("starts_at"), time.Now()).
		Where("? IS NULL OR ? > ?", bun.Ident("ends_at"), bun.Ident("ends_at"), time.Now()).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("get client claims: %w", err)
	}
	return claims, nil
}
