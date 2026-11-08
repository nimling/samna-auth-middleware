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

func PutGuestUser(ctx context.Context, appID uuid.UUID, claimID uuid.UUID, userID uuid.UUID) (*shared.UserClaim, error) {
	var payload []byte
	if err := instance.NewRaw("SELECT claimius.upsert_guest_user(?, ?, ?, ?)", appID, claimID, userID, GetGuestUserID(appID, claimID)).Scan(ctx, &payload); err != nil {
		var pgErr pgdriver.Error
		if errors.As(err, &pgErr) {
			switch pgErr.Field('C') {
			case "P0404":
				return nil, shared.ErrNotFound
			case "P0403":
				return nil, shared.ErrNoAccess
			}
		}
		return nil, fmt.Errorf("upsert guest user: %w", err)
	}
	var guest struct {
		UserClaim shared.UserClaim `json:"user_claim"`
	}
	if err := json.Unmarshal(payload, &guest); err != nil {
		return nil, fmt.Errorf("read guest grant: %w", err)
	}
	return &guest.UserClaim, nil
}
