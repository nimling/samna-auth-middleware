package db

import (
	"context"

	"github.com/google/uuid"
)

func (d *DB) GetObjectFields(ctx context.Context) ([]ObjectField, error) {
	var fields []ObjectField
	err := d.NewSelect().
		Model(&fields).
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
	var fields []UserField
	err := d.NewSelect().
		Model(&fields).
		Where("field_id = ?", fieldID).
		Where("sa_deleted_at IS NULL").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return fields, nil
}

func (d *DB) PostObjectField(ctx context.Context, field *ObjectField) error {
	if field.AppID == (uuid.UUID{}) {
		app, err := d.GetApp(ctx)
		if err != nil {
			return err
		}
		field.AppID = app.ID
		if field.OwnerID == (uuid.UUID{}) {
			field.OwnerID = app.OwnerID
		}
	}
	q := d.NewInsert().Model(field).Returning("*")
	if field.ID == (uuid.UUID{}) {
		q = q.ExcludeColumn("id")
	}
	_, err := q.Exec(ctx)
	return err
}

func (d *DB) PutObjectField(ctx context.Context, fieldID uuid.UUID, field *ObjectField) error {
	_, err := d.NewUpdate().
		Model(field).
		Where("id = ?", fieldID).
		Where("sa_deleted_at IS NULL").
		OmitZero().
		Exec(ctx)
	return err
}

func (d *DB) DeleteObjectField(ctx context.Context, fieldID uuid.UUID) error {
	_, err := d.NewUpdate().
		Model((*ObjectField)(nil)).
		Set("sa_deleted_at = NOW()").
		Where("id = ?", fieldID).
		Where("sa_deleted_at IS NULL").
		Exec(ctx)
	return err
}

func (d *DB) PostUserField(ctx context.Context, field *UserField) error {
	if field.AppID == (uuid.UUID{}) {
		app, err := d.GetApp(ctx)
		if err != nil {
			return err
		}
		field.AppID = app.ID
		if field.OwnerID == (uuid.UUID{}) {
			field.OwnerID = app.OwnerID
		}
	}
	q := d.NewInsert().Model(field).Returning("*")
	if field.ID == (uuid.UUID{}) {
		q = q.ExcludeColumn("id")
	}
	_, err := q.Exec(ctx)
	return err
}

func (d *DB) PutUserField(ctx context.Context, valueID uuid.UUID, field *UserField) error {
	_, err := d.NewUpdate().
		Model(field).
		Where("id = ?", valueID).
		Where("sa_deleted_at IS NULL").
		OmitZero().
		Exec(ctx)
	return err
}

func (d *DB) DeleteUserField(ctx context.Context, valueID uuid.UUID) error {
	_, err := d.NewUpdate().
		Model((*UserField)(nil)).
		Set("sa_deleted_at = NOW()").
		Where("id = ?", valueID).
		Where("sa_deleted_at IS NULL").
		Exec(ctx)
	return err
}
