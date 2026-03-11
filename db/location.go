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

func (d *DB) GetLocations(ctx context.Context, relations ...RelationFunc) ([]Location, error) {
	var locs []Location
	q := d.NewSelect().
		Model(&locs).
		Where("sa_deleted_at IS NULL")
	q = ApplyRelations(q, relations)
	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}
	return locs, nil
}

func (d *DB) CreateLocation(ctx context.Context, loc *Location) error {
	_, err := d.NewInsert().
		Model(loc).
		Exec(ctx)
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
	_, err := d.NewRaw(
		"UPDATE samna_auth.location SET sa_deleted_at = CURRENT_TIMESTAMP WHERE id = ? AND sa_deleted_at IS NULL",
		locationID,
	).Exec(ctx)
	return err
}
