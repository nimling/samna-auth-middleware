package sdb

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func profileError(err error) error {
	var pgErr pgdriver.Error
	if errors.As(err, &pgErr) {
		switch pgErr.Field('C') {
		case "P0404":
			return fmt.Errorf("%w: %s", shared.ErrNotFound, pgErr.Field('M'))
		case "22023":
			return fmt.Errorf("%w: %s", shared.ErrBadProfileItem, pgErr.Field('M'))
		}
	}
	return err
}

func PutProfileItem(ctx context.Context, appID uuid.UUID, item shared.BodySamProfileItem) (*shared.ProfileItem, error) {
	items := make([]shared.ProfileItem, 0, 1)
	if err := instance.NewRaw(`SELECT p.id, p.app_id, a.slug::TEXT AS app_slug, a.name::TEXT AS app_name, p.user_id, p.object_type, p.object_id, p.field, p.label, p.value, p.link, p."index", p.sa_created_at, p.sa_updated_at
		FROM claimius.upsert_profile_item(?, ?, ?, ?, ?, ?, ?, ?, ?) p
		JOIN claimius.samna_app a ON a.id = p.app_id`,
		appID, item.UserID, item.ObjectType, item.ObjectID, item.Field, item.Label, item.Value, item.Link, item.Index,
	).Scan(ctx, &items); err != nil {
		return nil, profileError(err)
	}
	if len(items) == 0 {
		return nil, shared.ErrNotFound
	}
	return &items[0], nil
}

func DeleteProfileItem(ctx context.Context, appID uuid.UUID, key shared.BodySamProfileItemKey) ([]shared.ProfileItem, error) {
	items := make([]shared.ProfileItem, 0)
	if err := instance.NewRaw(`SELECT id, app_id, user_id, object_type, object_id, field, label, value, link, "index", sa_created_at, sa_updated_at FROM claimius.delete_profile_item(?, ?, ?, ?, ?)`,
		appID, key.UserID, key.ObjectType, key.ObjectID, key.Field,
	).Scan(ctx, &items); err != nil {
		return nil, profileError(err)
	}
	return items, nil
}

func GetProfileItems(ctx context.Context, userID uuid.UUID, appID *uuid.UUID) ([]shared.ProfileItem, error) {
	items := make([]shared.ProfileItem, 0)
	if err := instance.NewRaw(`SELECT * FROM claimius.get_profile_items(?, ?)`, userID, appID).Scan(ctx, &items); err != nil {
		return nil, profileError(err)
	}
	return items, nil
}
