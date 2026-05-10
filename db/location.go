package db

import (
	"context"

	"github.com/google/uuid"
)

func (d *DB) GetLocation(ctx context.Context, locationID uuid.UUID, relations ...RelationFunc) (*Location, error) {
	var loc Location
	q := d.NewSelect().
		Model(&loc).
		ExcludeColumn("sa_deleted_at").
		Where("id = ?", locationID).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("app_id").
		Limit(1)
	q = ApplyRelations(q, relations)
	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &loc, nil
}

func (d *DB) GetLocations(ctx context.Context, userID uuid.UUID, requiredAccess ClaimAccess, relations ...RelationFunc) ([]Location, error) {
	locs := make([]Location, 0)
	q := d.NewSelect().
		Model(&locs).
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where(ObjectWhere(userID, OBJECT_TYPE_LOCATION, requiredAccess))
	q = ApplyRelations(q, relations)
	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}
	return locs, nil
}

func (d *DB) GetChildLocations(ctx context.Context, startID uuid.UUID) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0)
	err := d.NewRaw(`
		SELECT object_id
		FROM claimius.get_subtree(
			'location',
			(SELECT sa_root_id FROM claimius.location WHERE id = ? AND sa_deleted_at IS NULL),
			?,
			?
		)
		WHERE object_type = ?
		  AND object_id <> ?
	`, startID, OBJECT_TYPE_LOCATION, startID, OBJECT_TYPE_LOCATION, startID).
		Scan(ctx, &ids)
	return ids, err
}

func (d *DB) GetParentLocations(ctx context.Context, startID uuid.UUID) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0)
	err := d.NewRaw(`
		SELECT object_id
		FROM claimius.get_parenthood_ancestors(?, ?)
		WHERE hop > 0
	`, OBJECT_TYPE_LOCATION, startID).
		Scan(ctx, &ids)
	return ids, err
}

func (d *DB) PostLocation(ctx context.Context, loc *Location) error {
	_, err := d.NewInsert().
		Model(loc).
		ExcludeColumn("id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		Value("app_id", "claimius.get_disciple_app_id()").
		Returning("*").
		Exec(ctx, loc)
	return err
}

func (d *DB) PutLocation(ctx context.Context, locationID uuid.UUID, loc *Location) error {
	_, err := d.NewUpdate().
		Model(loc).
		Where("id = ?", locationID).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("id", "app_id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		OmitZero().
		Returning("*").
		Exec(ctx, loc)
	return err
}

func (d *DB) DeleteLocation(ctx context.Context, locationID uuid.UUID) error {
	_, err := d.NewUpdate().
		Model((*Location)(nil)).
		Where("id = ?", locationID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Exec(ctx)
	return err
}

func (d *DB) GetLocationAudit(ctx context.Context, locationID uuid.UUID) ([]Audit, error) {
	audits := make([]Audit, 0)
	err := d.NewSelect().
		Model(&audits).
		ExcludeColumn("sa_deleted_at").
		Where("object_id = ?", locationID).
		Where("object_type = ?", OBJECT_TYPE_LOCATION).
		ExcludeColumn("app_id").
		OrderExpr("sa_created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return audits, nil
}
