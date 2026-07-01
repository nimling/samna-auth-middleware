package sdb

import (
	"context"

	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/internal/store"
	"github.com/nimling/samna-auth-middleware/shared"
)

func (d *DB) IssueUserToken(ctx context.Context, ac *AccessClaim, payload SamnaUserToken) (*SamnaUser, bool, error) {
	if shared.GetDebugMode() != shared.DebugModeNone {
		return d.DB.IssueUserToken(ctx, ac, payload)
	}
	var user SamnaUser
	if err := client.Post(ctx, "user/token", payload, &user); err != nil {
		return nil, false, err
	}
	if err := store.Upsert(ctx, d.DB.DB, &user); err != nil {
		return nil, false, err
	}
	return &user, true, nil
}

func (d *DB) UpsertUserExternal(ctx context.Context, ac *AccessClaim, payload SamnaUserExternal) (*SamnaUser, bool, error) {
	if shared.GetDebugMode() != shared.DebugModeNone {
		return d.DB.UpsertUserExternal(ctx, ac, payload)
	}
	var user SamnaUser
	if err := client.Post(ctx, "user/external", payload, &user); err != nil {
		return nil, false, err
	}
	if err := store.Upsert(ctx, d.DB.DB, &user); err != nil {
		return nil, false, err
	}
	return &user, true, nil
}
