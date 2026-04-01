package db

import (
	"context"

	"github.com/google/uuid"
)

func (d *DB) GetLocationByID(ctx context.Context, locationID uuid.UUID, relations ...RelationFunc) (*Location, error) {
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
	var locs []Location
	q := d.NewSelect().
		Model(&locs).
		Where("location.id IN (SELECT id FROM samna_auth.get_locations(?, ?))", userID, accessLevel)
	q = ApplyRelations(q, relations)
	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}
	return locs, nil
}

func (d *DB) GetChildLocations(ctx context.Context, startID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := d.NewRaw("SELECT id FROM samna_auth.get_child_locations(?::uuid)", startID).
		Scan(ctx, &ids)
	return ids, err
}

func (d *DB) GetParentLocations(ctx context.Context, startID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := d.NewRaw("SELECT id FROM samna_auth.get_parent_locations(?::uuid)", startID).
		Scan(ctx, &ids)
	return ids, err
}

func (d *DB) CreateLocation(ctx context.Context, loc *Location) error {
	q := d.NewInsert().
		Model(loc).
		Returning("*")
	if loc.ID == (uuid.UUID{}) {
		q = q.ExcludeColumn("id")
	}
	_, err := q.Exec(ctx, loc)
	return err
}

func (d *DB) UpdateLocation(ctx context.Context, locationID uuid.UUID, loc *Location) error {
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
	var audits []Audit
	err := d.NewSelect().
		Model(&audits).
		Where("object_type = ?", "location").
		Where("object_id = ?", locationID).
		OrderExpr("sa_created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return audits, nil
}
