package db

import (
	"context"

	"github.com/google/uuid"
)

func (d *DB) GetOrganization(ctx context.Context, orgID uuid.UUID, relations ...RelationFunc) (*Organization, error) {
	var org Organization
	q := d.NewSelect().ExcludeColumn("app_id").
		Model(&org).
		Where("id = ?", orgID).
		Where("sa_deleted_at IS NULL").
		Limit(1)
	q = ApplyRelations(q, relations)
	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &org, nil
}

func (d *DB) GetOrganizations(ctx context.Context, userID uuid.UUID, accessLevel ClaimAccessLevel, relations ...RelationFunc) ([]Organization, error) {
	orgs := make([]Organization, 0)
	q := d.NewSelect().ExcludeColumn("app_id").
		Model(&orgs).
		Where("organization.id IN (SELECT object_id FROM claimius.get_objects(?, claimius.get_disciple_app_id(), ?, ?))", userID, OBJECT_TYPE_ORGANIZATION, accessLevel)
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
		WITH RECURSIVE descendants AS (
			SELECT id, sa_owner_id
			FROM claimius.organization
			WHERE id = ?
			  AND sa_deleted_at IS NULL
			UNION ALL
			SELECT o.id, o.sa_owner_id
			FROM claimius.organization o
			JOIN descendants d ON o.sa_owner_id = d.id
			WHERE o.id <> o.sa_owner_id
			  AND o.sa_deleted_at IS NULL
		)
		SELECT id FROM descendants WHERE id <> ?
	`, startID, startID).
		Scan(ctx, &ids)
	return ids, err
}

func (d *DB) GetParentOrganizations(ctx context.Context, startID uuid.UUID) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0)
	err := d.NewRaw(`
		WITH RECURSIVE ancestors AS (
			SELECT id, sa_owner_id
			FROM claimius.organization
			WHERE id = ?
			  AND sa_deleted_at IS NULL
			UNION ALL
			SELECT o.id, o.sa_owner_id
			FROM claimius.organization o
			JOIN ancestors a ON a.sa_owner_id = o.id
			WHERE a.id <> a.sa_owner_id
			  AND o.sa_deleted_at IS NULL
		)
		SELECT id FROM ancestors WHERE id <> ?
	`, startID, startID).
		Scan(ctx, &ids)
	return ids, err
}

func (d *DB) PostOrganization(ctx context.Context, org *Organization) error {
	if org.AppID == (uuid.UUID{}) {
		app, err := d.GetApp(ctx)
		if err != nil {
			return err
		}
		org.AppID = app.ID
	}
	q := d.NewInsert().
		Model(org).
		Returning("*")
	if org.ID == (uuid.UUID{}) {
		q = q.ExcludeColumn("id")
	}
	_, err := q.Exec(ctx, org)
	return err
}

func (d *DB) PutOrganization(ctx context.Context, orgID uuid.UUID, org *Organization) error {
	_, err := d.NewUpdate().
		Model(org).
		Where("id = ?", orgID).
		Where("sa_deleted_at IS NULL").
		OmitZero().
		Exec(ctx)
	return err
}

func (d *DB) DeleteOrganization(ctx context.Context, orgID uuid.UUID) error {
	_, err := d.NewUpdate().
		Model((*Organization)(nil)).
		Set("sa_deleted_at = NOW()").
		Where("id = ?", orgID).
		Where("sa_deleted_at IS NULL").
		Exec(ctx)
	return err
}

func (d *DB) GetOrganizationAudit(ctx context.Context, orgID uuid.UUID) ([]Audit, error) {
	audits := make([]Audit, 0)
	err := d.NewSelect().ExcludeColumn("app_id").
		Model(&audits).
		Where("object_type = ?", OBJECT_TYPE_ORGANIZATION).
		Where("object_id = ?", orgID).
		OrderExpr("sa_created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return audits, nil
}
