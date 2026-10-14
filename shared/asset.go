package shared

import (
	"context"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
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
		Join("JOIN object_asset AS oa ON oa.asset_id = ?TableAlias.id AND oa.sa_deleted_at IS NULL").
		Where("oa.object_type = ?", OBJECT_TYPE_USER).
		Where("oa.object_id = ?", userID).
		Where("?TableAlias.sa_deleted_at IS NULL").
		OrderExpr("?TableAlias.\"index\" ASC, ?TableAlias.sa_created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return assets, nil
}

func (d *DB) insertAsset(ctx context.Context, ac *AccessClaim, asset *Asset) (*Asset, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	_, err := d.DB.NewInsert().
		Model(asset).
		ExcludeColumn("id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		Returning("*").
		Exec(ctx, asset)
	if err != nil {
		return nil, err
	}
	return asset, nil
}

func (d *DB) updateAsset(ctx context.Context, ac *AccessClaim, assetID uuid.UUID, asset *Asset) (*Asset, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	q := d.DB.NewUpdate().
		Model(asset).
		Where("id = ?", assetID).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("id", "user_id", "blob_url", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		OmitZero().
		Returning("*")
	for _, column := range asset.PatchNulls() {
		q = q.Set("? = NULL", bun.Ident(column))
	}
	_, err := q.Exec(ctx, asset)
	if err != nil {
		return nil, err
	}
	return asset, nil
}

func (d *DB) deleteAsset(ctx context.Context, ac *AccessClaim, assetID uuid.UUID) (*Asset, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	var asset Asset
	_, err := d.DB.NewUpdate().
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

func (d *DB) GetObjectAssets(ctx context.Context, userID uuid.UUID, objectType string, objectID uuid.UUID, kind string) ([]ObjectAsset, error) {
	links := make([]ObjectAsset, 0)
	var kindArg *string
	if kind != "" {
		kindArg = &kind
	}
	if err := d.NewRaw("SELECT * FROM claimius.get_object_assets(?, ?, ?, ?)", userID, objectType, objectID, kindArg).Scan(ctx, &links); err != nil {
		return nil, err
	}
	return links, nil
}

func (d *DB) GetObjectAsset(ctx context.Context, userID, linkID uuid.UUID) (*ObjectAsset, error) {
	links := make([]ObjectAsset, 0, 1)
	if err := d.NewRaw(`
		SELECT a.*
		FROM claimius.object_asset l
		CROSS JOIN LATERAL claimius.get_object_assets(?, l.object_type, l.object_id) a
		WHERE l.id = ?
		  AND a.id = l.id`, userID, linkID).Scan(ctx, &links); err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return nil, ErrNotFound
	}
	return &links[0], nil
}
