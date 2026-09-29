package shared

import (
	"context"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

func (d *DB) GetOrganization(ctx context.Context, orgID uuid.UUID) (*Organization, error) {
	var org Organization
	err := d.GetOrganizationQuery(&org, orgID).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &org, nil
}

func (d *DB) GetOrganizationQuery(org *Organization, orgID uuid.UUID) *bun.SelectQuery {
	return d.NewSelect().
		Model(org).
		ExcludeColumn("sa_deleted_at").
		Where("organization.id = ?", orgID).
		Where("organization.sa_deleted_at IS NULL").
		Limit(1)
}

func (d *DB) GetOrganizations(ctx context.Context, userID uuid.UUID, requiredAccess ClaimAccess, after *uuid.UUID, limit int) ([]Organization, error) {
	orgs := make([]Organization, 0)
	err := d.GetOrganizationsQuery(&orgs, userID, requiredAccess, after, limit).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return orgs, nil
}

func (d *DB) GetOrganizationsQuery(orgs *[]Organization, userID uuid.UUID, requiredAccess ClaimAccess, after *uuid.UUID, limit int) *bun.SelectQuery {
	return ObjectWhere(d.NewSelect().
		Model(orgs).
		Where("organization.sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at"), userID, string(OBJECT_TYPE_ORGANIZATION), requiredAccess, after, limit)
}

func (d *DB) GetChildOrganizations(ctx context.Context, startID uuid.UUID) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0)
	err := d.NewRaw(`
		SELECT object_id
		FROM claimius.get_subtree(
			'ownership',
			(SELECT sa_root_id FROM claimius.organization WHERE id = ? AND sa_deleted_at IS NULL),
			?,
			?
		)
		WHERE object_type = ?
		  AND object_id <> ?
	`, startID, OBJECT_TYPE_ORGANIZATION, startID, OBJECT_TYPE_ORGANIZATION, startID).
		Scan(ctx, &ids)
	return ids, err
}

func (d *DB) GetParentOrganizations(ctx context.Context, startID uuid.UUID) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0)
	err := d.NewRaw(`
		SELECT object_id
		FROM claimius.get_owner_ancestors(?, ?)
		WHERE hop > 0
	`, OBJECT_TYPE_ORGANIZATION, startID).
		Scan(ctx, &ids)
	return ids, err
}

func (d *DB) insertOrganization(ctx context.Context, ac *AccessClaim, org *Organization) (*Organization, error) {
	if ac == nil || ac.ClaimID == nil || ac.UserClaimID == nil {
		return nil, ErrNoAccess
	}
	org.CreatedBy = *ac.UserClaimID
	bindingOwnerID := org.OwnerID
	if bindingOwnerID == uuid.Nil {
		bindingOwnerID = ac.OwnerID
	}
	excluded := []string{"sa_created_at", "sa_updated_at", "sa_deleted_at"}
	if org.ID == uuid.Nil {
		excluded = append(excluded, "id")
	}
	err := d.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().
			Model(org).
			ExcludeColumn(excluded...).
			Returning("*").
			Exec(ctx, org); err != nil {
			return err
		}
		if org.OwnerID == org.ID {
			return nil
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
				p_object_type   := 'claimius.organization',
				p_sa_access     := ?,
				p_inherits      := TRUE
			)
		`, *ac.ClaimID, bindingOwnerID, bindingRootID, *ac.UserClaimID, org.ID, int(ACCESS_OWNER|ACCESS_WRITE|ACCESS_READ)).Exec(ctx); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return org, nil
}

func (d *DB) InsertRootOrganization(ctx context.Context, ac *AccessClaim, org *Organization) (*Organization, error) {
	if ac == nil || ac.UserClaimID == nil {
		return nil, ErrNoAccess
	}
	org.ID = uuid.New()
	org.OwnerID = org.ID
	org.RootID = org.ID
	org.CreatedBy = *ac.UserClaimID
	org.Level = 0
	if err := d.NewInsert().Model(org).Access(ac).Scan(ctx, org); err != nil {
		return nil, err
	}
	return org, nil
}

func (d *DB) updateOrganization(ctx context.Context, ac *AccessClaim, orgID uuid.UUID, org *Organization) (*Organization, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	q := d.DB.NewUpdate().
		Model(org).
		Where("id = ?", orgID).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("id", "sa_owner_id", "sa_root_id", "sa_created_by", "provider_id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		OmitZero().
		Returning("*")
	for _, column := range org.PatchNulls() {
		q = q.Set("? = NULL", bun.Ident(column))
	}
	_, err := q.Exec(ctx, org)
	if err != nil {
		return nil, err
	}
	return org, nil
}

func (d *DB) deleteOrganization(ctx context.Context, ac *AccessClaim, orgID uuid.UUID) (*Organization, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	var org Organization
	_, err := d.DB.NewUpdate().
		Model(&org).
		Where("id = ?", orgID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Returning("*").
		Exec(ctx, &org)
	if err != nil {
		return nil, err
	}
	return &org, nil
}

func (d *DB) GetOrganizationAudit(ctx context.Context, actorID, orgID uuid.UUID, requiredAccess ClaimAccess) ([]Audit, error) {
	audits := make([]Audit, 0)
	err := d.NewSelect().
		TableExpr("claimius.get_audit(?, ?) AS audit", actorID, requiredAccess).
		Column("audit.*").
		Where("audit.object_id = ?", orgID).
		Where("audit.object_type = ?", OBJECT_TYPE_ORGANIZATION).
		OrderExpr("audit.sa_created_at DESC").
		Scan(ctx, &audits)
	if err != nil {
		return nil, err
	}
	return audits, nil
}
