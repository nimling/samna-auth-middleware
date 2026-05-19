package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

func (d *DB) GetLocation(ctx context.Context, locationID uuid.UUID) (*Location, error) {
	var loc Location
	err := d.GetLocationQuery(&loc, locationID).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &loc, nil
}

func (d *DB) GetLocationQuery(loc *Location, locationID uuid.UUID) *bun.SelectQuery {
	return d.NewSelect().
		Model(loc).
		ExcludeColumn("sa_deleted_at").
		Where("location.id = ?", locationID).
		Where("location.sa_deleted_at IS NULL").
		ExcludeColumn("app_id").
		Limit(1)
}

func (d *DB) GetLocations(ctx context.Context, userID uuid.UUID, requiredAccess ClaimAccess) ([]Location, error) {
	locs := make([]Location, 0)
	err := d.GetLocationsQuery(&locs, userID, requiredAccess).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return locs, nil
}

func (d *DB) GetLocationsQuery(locs *[]Location, userID uuid.UUID, requiredAccess ClaimAccess) *bun.SelectQuery {
	return d.NewSelect().
		Model(locs).
		Where("location.sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where(ObjectWhere(userID, OBJECT_TYPE_LOCATION, requiredAccess))
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

func (d *DB) PostLocation(ctx context.Context, ac *AccessClaim, loc *Location) (*Location, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	loc.CreatedBy = *ac.UserClaimID
	_, err := d.NewInsert().
		Model(loc).
		ExcludeColumn("id", "sa_root_id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		Value("app_id", "claimius.get_disciple_app_id()").
		Returning("*").
		Exec(ctx, loc)
	if err != nil {
		return nil, err
	}
	return loc, nil
}

func (d *DB) PutLocation(ctx context.Context, ac *AccessClaim, locationID uuid.UUID, loc *Location) (*Location, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	_, err := d.NewUpdate().
		Model(loc).
		Where("id = ?", locationID).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("id", "app_id", "sa_owner_id", "sa_root_id", "sa_parent_id", "sa_created_by", "provider_id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		Returning("*").
		Exec(ctx, loc)
	if err != nil {
		return nil, err
	}
	return loc, nil
}

func (d *DB) PatchLocation(ctx context.Context, ac *AccessClaim, locationID uuid.UUID, loc *Location) (*Location, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	q := d.NewUpdate().
		Model(loc).
		Where("id = ?", locationID).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("id", "app_id", "sa_owner_id", "sa_root_id", "sa_parent_id", "sa_created_by", "provider_id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		OmitZero().
		Returning("*")
	for _, column := range loc.PatchNulls() {
		q = q.Set("? = NULL", bun.Ident(column))
	}
	_, err := q.Exec(ctx, loc)
	if err != nil {
		return nil, err
	}
	return loc, nil
}

func (d *DB) DeleteLocation(ctx context.Context, ac *AccessClaim, locationID uuid.UUID) (*Location, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	var loc Location
	_, err := d.NewUpdate().
		Model(&loc).
		Where("id = ?", locationID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Returning("*").
		Exec(ctx, &loc)
	if err != nil {
		return nil, err
	}
	return &loc, nil
}

func (d *DB) GetLocationAudit(ctx context.Context, actorID, locationID uuid.UUID, requiredAccess ClaimAccess) ([]Audit, error) {
	audits := make([]Audit, 0)
	err := d.NewSelect().
		TableExpr("claimius.get_audit(?, claimius.get_disciple_app_id(), ?) AS audit", actorID, requiredAccess).
		Column("audit.*").
		Where("audit.object_id = ?", locationID).
		Where("audit.object_type = ?", OBJECT_TYPE_LOCATION).
		OrderExpr("audit.sa_created_at DESC").
		Scan(ctx, &audits)
	if err != nil {
		return nil, err
	}
	return audits, nil
}
