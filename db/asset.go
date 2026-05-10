package db

import (
	"context"

	"github.com/google/uuid"
)

func (d *DB) GetAssets(ctx context.Context) ([]Asset, error) {
	assets := make([]Asset, 0)
	err := d.NewSelect().
		Model(&assets).
		ExcludeColumn("sa_deleted_at").
		Where("sa_deleted_at IS NULL").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return assets, nil
}

func (d *DB) GetAsset(ctx context.Context, assetID uuid.UUID) (*Asset, error) {
	var asset Asset
	err := d.NewSelect().
		Model(&asset).
		ExcludeColumn("sa_deleted_at").
		Where("id = ?", assetID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &asset, nil
}

func (d *DB) GetUserAssets(ctx context.Context, userID uuid.UUID) ([]Asset, error) {
	assets := make([]Asset, 0)
	err := d.NewSelect().
		Model(&assets).
		ExcludeColumn("sa_deleted_at").
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		OrderExpr("index ASC, sa_created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return assets, nil
}

func (d *DB) PostAsset(ctx context.Context, asset *Asset) error {
	_, err := d.NewInsert().
		Model(asset).
		ExcludeColumn("id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		Returning("*").
		Exec(ctx, asset)
	return err
}

func (d *DB) PutAsset(ctx context.Context, assetID uuid.UUID, asset *Asset) error {
	_, err := d.NewUpdate().
		Model(asset).
		Where("id = ?", assetID).
		Where("sa_deleted_at IS NULL").
		OmitZero().
		Returning("*").
		Exec(ctx, asset)
	return err
}

func (d *DB) DeleteAsset(ctx context.Context, assetID uuid.UUID) (*Asset, error) {
	var asset Asset
	_, err := d.NewUpdate().
		Model(&asset).
		Set("sa_deleted_at = NOW()").
		Where("id = ?", assetID).
		Where("sa_deleted_at IS NULL").
		Returning("*").
		Exec(ctx, &asset)
	if err != nil {
		return nil, err
	}
	return &asset, nil
}
