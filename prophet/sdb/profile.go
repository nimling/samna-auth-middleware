package sdb

import (
	"context"
	"encoding/json"
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

func PutProfileItem(ctx context.Context, appID uuid.UUID, item shared.BodySamProfileItem) (json.RawMessage, error) {
	body, err := json.Marshal(item.Item())
	if err != nil {
		return nil, err
	}
	var profile string
	if err := instance.NewRaw(`SELECT claimius.upsert_profile_item(?, ?, ?, ?::jsonb)::text`, appID, item.UserID, item.Field, string(body)).Scan(ctx, &profile); err != nil {
		return nil, profileError(err)
	}
	return json.RawMessage(profile), nil
}

func DeleteProfileItem(ctx context.Context, appID uuid.UUID, key shared.BodySamProfileItemKey) (json.RawMessage, error) {
	var profile string
	if err := instance.NewRaw(`SELECT claimius.delete_profile_item(?, ?, ?)::text`, appID, key.UserID, key.Field).Scan(ctx, &profile); err != nil {
		return nil, profileError(err)
	}
	return json.RawMessage(profile), nil
}

func PutProfile(ctx context.Context, userID uuid.UUID, items map[string]*shared.ProfileItem) (json.RawMessage, error) {
	for key, item := range items {
		if item == nil {
			continue
		}
		if err := item.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %s", err, key)
		}
	}
	body, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	var profile string
	if err := instance.NewRaw(`SELECT claimius.update_profile(?, ?::jsonb)::text`, userID, string(body)).Scan(ctx, &profile); err != nil {
		return nil, profileError(err)
	}
	return json.RawMessage(profile), nil
}
