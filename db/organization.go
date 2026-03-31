package db

import (
	"context"

	"github.com/google/uuid"
)

func (d *DB) GetOrganizationByID(ctx context.Context, orgID uuid.UUID, relations ...RelationFunc) (*Organization, error) {
	var org Organization
	q := d.NewSelect().
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
	var orgs []Organization
	q := d.NewSelect().
		Model(&orgs).
		Where("organization.id IN (SELECT id FROM samna_auth.get_organizations(?, ?))", userID, accessLevel)
	q = ApplyRelations(q, relations)
	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}
	return orgs, nil
}

func (d *DB) GetChildOrganizations(ctx context.Context, startID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := d.NewRaw("SELECT id FROM samna_auth.get_child_organizations(?::uuid)", startID).
		Scan(ctx, &ids)
	return ids, err
}

func (d *DB) GetParentOrganizations(ctx context.Context, startID uuid.UUID) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := d.NewRaw("SELECT id FROM samna_auth.get_parent_organizations(?::uuid)", startID).
		Scan(ctx, &ids)
	return ids, err
}

func (d *DB) CreateOrganization(ctx context.Context, org *Organization) error {
	_, err := d.NewInsert().
		Model(org).
		Returning("*").
		Exec(ctx)
	return err
}

func (d *DB) UpdateOrganization(ctx context.Context, orgID uuid.UUID, org *Organization) error {
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
