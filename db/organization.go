package db

import (
	"context"

	"github.com/google/uuid"
)

func (d *DB) GetOrganization(ctx context.Context, orgID uuid.UUID, relations ...RelationFunc) (*Organization, error) {
	var org Organization
	q := d.NewSelect().
		Model(&org).
		ExcludeColumn("sa_deleted_at").
		Where("id = ?", orgID).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("app_id").
		Limit(1)
	q = ApplyRelations(q, relations)
	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &org, nil
}

func (d *DB) GetOrganizations(ctx context.Context, userID uuid.UUID, requiredAccess ClaimAccess, relations ...RelationFunc) ([]Organization, error) {
	orgs := make([]Organization, 0)
	q := d.NewSelect().
		Model(&orgs).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where(ObjectWhere(userID, OBJECT_TYPE_ORGANIZATION, requiredAccess))
	q = ApplyRelations(q, relations)
	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}
	return orgs, nil
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

func (d *DB) PostOrganization(ctx context.Context, org *Organization) error {
	_, err := d.NewInsert().
		Model(org).
		ExcludeColumn("id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		Value("app_id", "claimius.get_disciple_app_id()").
		Returning("*").
		Exec(ctx, org)
	return err
}

func (d *DB) PutOrganization(ctx context.Context, orgID uuid.UUID, org *Organization) error {
	_, err := d.NewUpdate().
		Model(org).
		Where("id = ?", orgID).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("id", "app_id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		OmitZero().
		Returning("*").
		Exec(ctx, org)
	return err
}

func (d *DB) DeleteOrganization(ctx context.Context, orgID uuid.UUID) (*Organization, error) {
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
