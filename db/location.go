package db

import (
	"context"

	"github.com/google/uuid"
)

func (d *DB) GetLocation(ctx context.Context, locationID uuid.UUID, relations ...RelationFunc) (*Location, error) {
	var loc Location
	q := d.NewSelect().
		Model(&loc).
		Where("id = ?", locationID).
		Where("sa_deleted_at IS NULL").
		Limit(1)
	q = ApplyRelations(q, relations)
	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &loc, nil
}

func (d *DB) GetLocations(ctx context.Context, userID uuid.UUID, accessLevel ClaimAccessLevel, relations ...RelationFunc) ([]Location, error) {
	locs := make([]Location, 0)
	q := d.NewSelect().
		Model(&locs).
		Where("location.id IN (SELECT object_id FROM claimius.get_objects(?, claimius.get_disciple_app_id(), ?, ?))", userID, OBJECT_TYPE_LOCATION, accessLevel)
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
		WITH RECURSIVE descendants AS (
			SELECT id, sa_parent_id
			FROM claimius.location
			WHERE id = ?
			  AND sa_deleted_at IS NULL
			UNION ALL
			SELECT l.id, l.sa_parent_id
			FROM claimius.location l
			JOIN descendants d ON l.sa_parent_id = d.id
			WHERE l.id <> l.sa_parent_id
			  AND l.sa_deleted_at IS NULL
		)
		SELECT id FROM descendants WHERE id <> ?
	`, startID, startID).
		Scan(ctx, &ids)
	return ids, err
}

func (d *DB) GetParentLocations(ctx context.Context, startID uuid.UUID) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0)
	err := d.NewRaw(`
		WITH RECURSIVE ancestors AS (
			SELECT id, sa_parent_id
			FROM claimius.location
			WHERE id = ?
			  AND sa_deleted_at IS NULL
			UNION ALL
			SELECT l.id, l.sa_parent_id
			FROM claimius.location l
			JOIN ancestors a ON a.sa_parent_id = l.id
			WHERE a.id <> a.sa_parent_id
			  AND l.sa_deleted_at IS NULL
		)
		SELECT id FROM ancestors WHERE id <> ?
	`, startID, startID).
		Scan(ctx, &ids)
	return ids, err
}

func (d *DB) PostLocation(ctx context.Context, loc *Location) error {
	if loc.AppID == (uuid.UUID{}) {
		app, err := d.GetApp(ctx)
		if err != nil {
			return err
		}
		loc.AppID = app.ID
	}
	q := d.NewInsert().
		Model(loc).
		Returning("*")
	if loc.ID == (uuid.UUID{}) {
		q = q.ExcludeColumn("id")
	}
	_, err := q.Exec(ctx, loc)
	return err
}

func (d *DB) PutLocation(ctx context.Context, locationID uuid.UUID, loc *Location) error {
	_, err := d.NewUpdate().
		Model(loc).
		Where("id = ?", locationID).
		Where("sa_deleted_at IS NULL").
		OmitZero().
		Exec(ctx)
	return err
}

func (d *DB) DeleteLocation(ctx context.Context, locationID uuid.UUID) error {
	_, err := d.NewUpdate().
		Model((*Location)(nil)).
		Set("sa_deleted_at = NOW()").
		Where("id = ?", locationID).
		Where("sa_deleted_at IS NULL").
		Exec(ctx)
	return err
}

func (d *DB) GetLocationAudit(ctx context.Context, locationID uuid.UUID) ([]Audit, error) {
	audits := make([]Audit, 0)
	err := d.NewSelect().
		Model(&audits).
		Where("object_type = ?", OBJECT_TYPE_LOCATION).
		Where("object_id = ?", locationID).
		OrderExpr("sa_created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return audits, nil
}
