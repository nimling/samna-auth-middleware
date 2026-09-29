package shared

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
		Limit(1)
}

func (d *DB) GetLocations(ctx context.Context, userID uuid.UUID, requiredAccess ClaimAccess, after *uuid.UUID, limit int) ([]Location, error) {
	locs := make([]Location, 0)
	err := d.GetLocationsQuery(&locs, userID, requiredAccess, after, limit).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return locs, nil
}

func (d *DB) GetLocationsQuery(locs *[]Location, userID uuid.UUID, requiredAccess ClaimAccess, after *uuid.UUID, limit int) *bun.SelectQuery {
	return ObjectWhere(d.NewSelect().
		Model(locs).
		Where("location.sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at"), userID, string(OBJECT_TYPE_LOCATION), requiredAccess, nil, after, limit)
}

func (d *DB) GetChildLocations(ctx context.Context, startID uuid.UUID) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0)
	err := d.Read(ctx, func(ctx context.Context, db *bun.DB) error {
		return db.NewRaw(`
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
	})
	return ids, err
}

func (d *DB) GetParentLocations(ctx context.Context, startID uuid.UUID) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0)
	err := d.Read(ctx, func(ctx context.Context, db *bun.DB) error {
		return db.NewRaw(`
		SELECT object_id
		FROM claimius.get_parenthood_ancestors(?, ?)
		WHERE hop > 0
	`, OBJECT_TYPE_LOCATION, startID).
			Scan(ctx, &ids)
	})
	return ids, err
}

func (d *DB) insertLocation(ctx context.Context, ac *AccessClaim, loc *Location) (*Location, error) {
	if ac == nil || ac.ClaimID == nil || ac.UserClaimID == nil {
		return nil, ErrNoAccess
	}
	loc.CreatedBy = *ac.UserClaimID
	bindingOwnerID := loc.OwnerID
	if bindingOwnerID == uuid.Nil {
		bindingOwnerID = ac.OwnerID
	}
	err := d.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().
			Model(loc).
			ExcludeColumn("id", "sa_root_id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
			Returning("*").
			Exec(ctx, loc); err != nil {
			return err
		}
		var bindingRootID uuid.UUID
		if err := tx.NewRaw(`SELECT sa_root_id FROM claimius.organization WHERE id = ? AND sa_deleted_at IS NULL`, bindingOwnerID).Scan(ctx, &bindingRootID); err != nil {
			return err
		}
		if _, err := tx.NewRaw(`
			SELECT claimius.assign_claim_object(
				p_claim_id      := ?,
				p_sa_owner_id   := ?,
				p_sa_root_id    := ?,
				p_sa_created_by := ?,
				p_object_id     := ?,
				p_object_type   := 'claimius.location',
				p_sa_access     := ?,
				p_inherits      := TRUE
			)
		`, *ac.ClaimID, bindingOwnerID, bindingRootID, *ac.UserClaimID, loc.ID, int(ACCESS_OWNER|ACCESS_WRITE|ACCESS_READ)).Exec(ctx); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return loc, nil
}

func (d *DB) updateLocation(ctx context.Context, ac *AccessClaim, locationID uuid.UUID, loc *Location) (*Location, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	q := d.DB.NewUpdate().
		Model(loc).
		Where("id = ?", locationID).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("id", "sa_owner_id", "sa_root_id", "sa_parent_id", "sa_created_by", "provider_id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
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

func (d *DB) deleteLocation(ctx context.Context, ac *AccessClaim, locationID uuid.UUID) (*Location, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	var loc Location
	_, err := d.DB.NewUpdate().
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
		TableExpr("claimius.get_audit(?, ?) AS audit", actorID, requiredAccess).
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
