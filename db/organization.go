package db

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
		ExcludeColumn("app_id").
		Limit(1)
}

func (d *DB) GetOrganizations(ctx context.Context, userID uuid.UUID, requiredAccess ClaimAccess) ([]Organization, error) {
	orgs := make([]Organization, 0)
	err := d.GetOrganizationsQuery(&orgs, userID, requiredAccess).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return orgs, nil
}

func (d *DB) GetOrganizationsQuery(orgs *[]Organization, userID uuid.UUID, requiredAccess ClaimAccess) *bun.SelectQuery {
	return d.NewSelect().
		Model(orgs).
		Where("organization.sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where(ObjectWhere(userID, OBJECT_TYPE_ORGANIZATION, requiredAccess))
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

func (d *DB) PostOrganization(ctx context.Context, ac *AccessClaim, org *Organization) (*Organization, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	org.CreatedBy = *ac.UserClaimID
	org.RootID = ac.RootID
	_, err := d.NewInsert().
		Model(org).
		ExcludeColumn("id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		Value("app_id", "claimius.get_disciple_app_id()").
		Returning("*").
		Exec(ctx, org)
	if err != nil {
		return nil, err
	}
	return org, nil
}

func (d *DB) PostRootOrganization(ctx context.Context, ac *AccessClaim, org *Organization) (*Organization, error) {
	if ac == nil || ac.UserClaimID == nil {
		return nil, ErrNoAccess
	}
	var id uuid.UUID
	err := d.NewRaw(`
		SELECT (claimius.create_root_organization(
			claimius.get_disciple_app_id(), ?, ?, ?, ?, ?
		) -> 'organization' ->> 'id')::uuid
	`,
		org.Name,
		*ac.UserClaimID,
		org.Description,
		org.Type,
		org.ID,
	).Scan(ctx, &id)
	if err != nil {
		return nil, err
	}
	err = d.NewSelect().
		Model(org).
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return org, nil
}

func (d *DB) PutOrganization(ctx context.Context, ac *AccessClaim, orgID uuid.UUID, org *Organization) (*Organization, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	_, err := d.NewUpdate().
		Model(org).
		Where("id = ?", orgID).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("id", "app_id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		OmitZero().
		Returning("*").
		Exec(ctx, org)
	if err != nil {
		return nil, err
	}
	return org, nil
}

func (d *DB) DeleteOrganization(ctx context.Context, ac *AccessClaim, orgID uuid.UUID) (*Organization, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	var org Organization
	_, err := d.NewUpdate().
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
		TableExpr("claimius.get_audit(?, claimius.get_disciple_app_id(), ?) AS audit", actorID, requiredAccess).
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
