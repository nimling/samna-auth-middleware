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
		Where("o.id IN (SELECT id FROM samna_auth.get_organizations(?, ?))", userID, accessLevel)
	q = ApplyRelations(q, relations)
	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}
	return orgs, nil
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
	_, err := d.NewRaw(
		"UPDATE samna_auth.organization SET sa_deleted_at = CURRENT_TIMESTAMP WHERE id = ? AND sa_deleted_at IS NULL",
		orgID,
	).Exec(ctx)
	return err
}
