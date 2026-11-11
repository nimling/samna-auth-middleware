package shared

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/lib/pq"
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
	q := d.NewSelect().
		Model(orgs).
		Where("organization.sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at")
	if after == nil && limit == 0 {
		q = q.OrderExpr("?TableAlias.id")
	}
	return ObjectWhere(q, userID, string(OBJECT_TYPE_ORGANIZATION), requiredAccess, nil, after, limit)
}

func (d *DB) GetChildOrganizations(ctx context.Context, startID uuid.UUID) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0)
	err := d.Read(ctx, func(ctx context.Context, db *bun.DB) error {
		return db.NewRaw(`
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
	})
	return ids, err
}

func (d *DB) GetParentOrganizations(ctx context.Context, startID uuid.UUID) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0)
	err := d.Read(ctx, func(ctx context.Context, db *bun.DB) error {
		return db.NewRaw(`
		SELECT object_id
		FROM claimius.get_owner_ancestors(?, ?)
		WHERE hop > 0
	`, OBJECT_TYPE_ORGANIZATION, startID).
			Scan(ctx, &ids)
	})
	return ids, err
}

func (d *DB) insertOrganization(ctx context.Context, conn bun.IConn, ac *AccessClaim, org *Organization) (*Organization, error) {
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
	write := func(ctx context.Context, tx bun.IConn) error {
		if _, err := d.DB.NewInsert().Conn(tx).
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
		if err := d.DB.NewRaw(`SELECT sa_root_id FROM claimius.organization WHERE id = ? AND sa_deleted_at IS NULL`, bindingOwnerID).Conn(tx).Scan(ctx, &bindingRootID); err != nil {
			return err
		}
		if _, err := d.DB.NewRaw(`
			SELECT claimius.insert_claim_object(
				p_claim_id      := ?,
				p_sa_owner_id   := ?,
				p_sa_root_id    := ?,
				p_sa_created_by := ?,
				p_object_id     := ?,
				p_object_type   := 'claimius.organization',
				p_sa_access     := ?,
				p_inherits      := TRUE
			)
		`, *ac.ClaimID, bindingOwnerID, bindingRootID, *ac.UserClaimID, org.ID, int(ACCESS_OWNER|ACCESS_WRITE|ACCESS_READ)).Conn(tx).Exec(ctx); err != nil {
			return err
		}
		return nil
	}
	var err error
	if conn != nil {
		err = write(ctx, conn)
	} else {
		err = d.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error { return write(ctx, tx) })
	}
	if err != nil {
		return nil, err
	}
	return org, nil
}

func (d *DB) InsertRootOrganization(ctx context.Context, conn bun.IConn, ac *AccessClaim, org *Organization) (*Organization, error) {
	if ac == nil || ac.UserClaimID == nil {
		return nil, ErrNoAccess
	}
	org.ID = uuid.New()
	org.OwnerID = org.ID
	org.RootID = org.ID
	org.CreatedBy = *ac.UserClaimID
	org.Level = 0
	if err := d.NewInsert().Conn(conn).Model(org).Access(ac).Scan(ctx, org); err != nil {
		return nil, err
	}
	return org, nil
}

func (d *DB) validateOrganizationParent(ctx context.Context, ac *AccessClaim, orgID, ownerID uuid.UUID) error {
	if ownerID == uuid.Nil {
		return nil
	}
	var parent struct {
		Moved   bool `bun:"moved"`
		Allowed bool `bun:"allowed"`
	}
	err := d.NewRaw(`
		SELECT o.sa_owner_id <> p.id AS moved,
		       o.sa_root_id = p.sa_root_id
		       AND NOT EXISTS (
		           SELECT 1 FROM claimius.get_owner_ancestors(?, p.id) a WHERE a.object_id = o.id
		       ) AS allowed
		FROM claimius.organization o, claimius.organization p
		WHERE o.id = ? AND o.sa_deleted_at IS NULL
		  AND p.id = ? AND p.sa_deleted_at IS NULL
	`, OBJECT_TYPE_ORGANIZATION, orgID, ownerID).Scan(ctx, &parent)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !parent.Moved {
		return nil
	}
	if !parent.Allowed {
		return ErrOrganizationParent
	}
	if ac == nil || !d.HasAccess(ctx, ac.UserID, ownerID, OBJECT_TYPE_ORGANIZATION, ACCESS_WRITE) {
		return ErrNoAccess
	}
	return nil
}

func (d *DB) updateOrganization(ctx context.Context, conn bun.IConn, ac *AccessClaim, orgID uuid.UUID, org *Organization) (*Organization, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	if err := d.validateOrganizationParent(ctx, ac, orgID, org.OwnerID); err != nil {
		return nil, err
	}
	q := d.DB.NewUpdate().Conn(conn).
		Model(org).
		Where("id = ?", orgID).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("id", "sa_root_id", "sa_created_by", "provider_id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
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

func (d *DB) deleteOrganization(ctx context.Context, conn bun.IConn, ac *AccessClaim, orgID uuid.UUID) (*Organization, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	var orgType OrganizationType
	if err := d.DB.NewRaw(`SELECT type FROM claimius.organization WHERE id = ? AND sa_deleted_at IS NULL`, orgID).Conn(conn).Scan(ctx, &orgType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if orgType == ORGANIZATION_TYPE_CUSTOMER {
		return nil, ErrOrganizationCustomer
	}
	var org Organization
	_, err := d.DB.NewUpdate().Conn(conn).
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

func (d *DB) GetOrganizationsMany(ctx context.Context, ids, ownerIDs []uuid.UUID, externalIDs []string) ([]Organization, error) {
	orgs := make([]Organization, 0)
	err := d.NewRaw("SELECT * FROM claimius.get_organizations(?::uuid[], ?::uuid[], ?::text[])", pq.Array(ids), pq.Array(ownerIDs), pq.Array(externalIDs)).Scan(ctx, &orgs)
	if err != nil {
		return nil, err
	}
	return orgs, nil
}
