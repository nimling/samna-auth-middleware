package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

func (d *DB) GetObjectFields(ctx context.Context) ([]ObjectField, error) {
	fields := make([]ObjectField, 0)
	err := d.NewSelect().
		Model(&fields).
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("sa_deleted_at IS NULL").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return fields, nil
}

func (d *DB) GetObjectField(ctx context.Context, fieldID uuid.UUID) (*ObjectField, error) {
	var field ObjectField
	err := d.NewSelect().
		Model(&field).
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("id = ?", fieldID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &field, nil
}

func (d *DB) GetUserFields(ctx context.Context, fieldID uuid.UUID) ([]UserField, error) {
	fields := make([]UserField, 0)
	err := d.NewSelect().
		Model(&fields).
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("field_id = ?", fieldID).
		Where("sa_deleted_at IS NULL").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return fields, nil
}

func (d *DB) insertObjectField(ctx context.Context, ac *AccessClaim, field *ObjectField) (*ObjectField, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	field.CreatedBy = ac.UserClaimID
	field.OwnerID = ac.OwnerID
	_, err := d.DB.NewInsert().
		Model(field).
		ExcludeColumn("id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		Value("app_id", "claimius.get_disciple_app_id()").
		Returning("*").
		Exec(ctx, field)
	if err != nil {
		return nil, err
	}
	return field, nil
}

func (d *DB) updateObjectField(ctx context.Context, ac *AccessClaim, fieldID uuid.UUID, field *ObjectField) (*ObjectField, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	q := d.DB.NewUpdate().
		Model(field).
		Where("id = ?", fieldID).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("id", "app_id", "sa_owner_id", "sa_created_by", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		OmitZero().
		Returning("*")
	for _, column := range field.PatchNulls() {
		q = q.Set("? = NULL", bun.Ident(column))
	}
	_, err := q.Exec(ctx, field)
	if err != nil {
		return nil, err
	}
	return field, nil
}

func (d *DB) deleteObjectField(ctx context.Context, ac *AccessClaim, fieldID uuid.UUID) (*ObjectField, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	var field ObjectField
	_, err := d.DB.NewUpdate().
		Model(&field).
		Where("id = ?", fieldID).
		Set("sa_deleted_at = NOW()").
		Where("sa_deleted_at IS NULL").
		Returning("*").
		Exec(ctx, &field)
	if err != nil {
		return nil, err
	}
	return &field, nil
}

func (d *DB) insertUserField(ctx context.Context, ac *AccessClaim, field *UserField) (*UserField, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	field.CreatedBy = ac.UserClaimID
	field.OwnerID = ac.OwnerID
	_, err := d.DB.NewInsert().
		Model(field).
		ExcludeColumn("id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		Value("app_id", "claimius.get_disciple_app_id()").
		Returning("*").
		Exec(ctx, field)
	if err != nil {
		return nil, err
	}
	return field, nil
}

func (d *DB) updateUserField(ctx context.Context, ac *AccessClaim, valueID uuid.UUID, field *UserField) (*UserField, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	q := d.DB.NewUpdate().
		Model(field).
		Where("id = ?", valueID).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("id", "app_id", "field_id", "user_id", "sa_owner_id", "sa_created_by", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		OmitZero().
		Returning("*")
	for _, column := range field.PatchNulls() {
		q = q.Set("? = NULL", bun.Ident(column))
	}
	_, err := q.Exec(ctx, field)
	if err != nil {
		return nil, err
	}
	return field, nil
}

func (d *DB) deleteUserField(ctx context.Context, ac *AccessClaim, valueID uuid.UUID) (*UserField, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	var field UserField
	_, err := d.DB.NewUpdate().
		Model(&field).
		Where("id = ?", valueID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Returning("*").
		Exec(ctx, &field)
	if err != nil {
		return nil, err
	}
	return &field, nil
}
