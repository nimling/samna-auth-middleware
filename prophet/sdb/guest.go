package sdb

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func PutGuestUser(ctx context.Context, appID uuid.UUID, body shared.BodyGuest) (json.RawMessage, error) {
	guestUserID := GetGuestUserID(appID, body.ClaimID)
	if body.FirstName != nil || body.LastName != nil || body.Email != nil || body.Phone != nil {
		guestUserID = uuid.New()
	}
	var raw json.RawMessage
	if err := instance.NewRaw("SELECT claimius.upsert_guest_user(?, ?, ?, ?, ?, ?, ?, ?)",
		appID, body.ClaimID, body.UserID, guestUserID, body.FirstName, body.LastName, body.Email, body.Phone,
	).Scan(ctx, (*[]byte)(&raw)); err != nil {
		return nil, codeError(err)
	}
	return raw, nil
}
