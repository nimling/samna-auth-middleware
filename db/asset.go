package db

import (
	"context"

	"github.com/google/uuid"
)

func (d *DB) GetAssets(ctx context.Context) ([]Asset, error) {
	var assets []Asset
	err := d.NewSelect().
		Model(&assets).
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
	var assets []Asset
	err := d.NewSelect().
		Model(&assets).
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
	q := d.NewInsert().Model(asset).Returning("*")
	if asset.ID == (uuid.UUID{}) {
		q = q.ExcludeColumn("id")
	}
	_, err := q.Exec(ctx)
	return err
}

func (d *DB) PutAsset(ctx context.Context, assetID uuid.UUID, asset *Asset) error {
	_, err := d.NewUpdate().
		Model(asset).
		Where("id = ?", assetID).
		Where("sa_deleted_at IS NULL").
		OmitZero().
		Exec(ctx)
	return err
}

func (d *DB) DeleteAsset(ctx context.Context, assetID uuid.UUID) error {
	_, err := d.NewUpdate().
		Model((*Asset)(nil)).
		Set("sa_deleted_at = NOW()").
		Where("id = ?", assetID).
		Where("sa_deleted_at IS NULL").
		Exec(ctx)
	return err
}
