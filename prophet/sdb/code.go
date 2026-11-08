package sdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func codeError(err error) error {
	var pgErr pgdriver.Error
	if errors.As(err, &pgErr) {
		switch pgErr.Field('C') {
		case "P0404":
			return fmt.Errorf("%w: %s", shared.ErrNotFound, pgErr.Field('M'))
		case "P0403":
			return fmt.Errorf("%w: %s", shared.ErrNoAccess, pgErr.Field('M'))
		case "22023":
			return fmt.Errorf("%w: %s", shared.ErrBadCode, pgErr.Field('M'))
		}
	}
	return err
}

func PostCode(ctx context.Context, appID uuid.UUID, body shared.BodySamCode) (json.RawMessage, error) {
	if body.Kind != shared.CODE_KIND_INVITE && body.Kind != shared.CODE_KIND_GUEST {
		return nil, fmt.Errorf("%w: kind must be invite or guest", shared.ErrBadCode)
	}
	if body.Count < 0 {
		return nil, fmt.Errorf("%w: count must not be negative", shared.ErrBadCode)
	}
	var data any
	if len(body.Data) > 0 {
		data = string(body.Data)
	}
	var raw json.RawMessage
	if err := instance.NewRaw("SELECT claimius.insert_code(?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?) -> 'code'",
		appID, body.UserID, body.ClaimID, body.Hash, string(body.Kind), body.ObjectID, body.ObjectType, data, body.StartsAt, body.EndsAt, body.Count,
	).Scan(ctx, (*[]byte)(&raw)); err != nil {
		return nil, codeError(err)
	}
	return raw, nil
}

func GetCodes(ctx context.Context, appID, claimID uuid.UUID) ([]shared.Code, error) {
	codes := make([]shared.Code, 0)
	if err := instance.NewRaw("SELECT id, hash, kind, claim_id, object_id, object_type, data, starts_at, ends_at, count, used_count, revoked_at, sa_owner_id, sa_created_by, sa_deleted_at, sa_created_at, sa_updated_at FROM claimius.get_codes(?, ?)", uuid.NullUUID{UUID: appID, Valid: appID != uuid.Nil}, uuid.NullUUID{UUID: claimID, Valid: claimID != uuid.Nil}).Scan(ctx, &codes); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return codes, nil
}

func DeleteCode(ctx context.Context, appID, userID, codeID uuid.UUID) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := instance.NewRaw("SELECT claimius.delete_code(?, ?, ?) -> 'code'", appID, userID, codeID).Scan(ctx, (*[]byte)(&raw)); err != nil {
		return nil, codeError(err)
	}
	return raw, nil
}

func GetCodeAccess(ctx context.Context, appID uuid.UUID, value string) (*shared.CodeAccess, error) {
	var raw []byte
	if err := instance.NewRaw("SELECT claimius.get_code(?, ?)", uuid.NullUUID{UUID: appID, Valid: appID != uuid.Nil}, shared.GetCodeHash(value)).Scan(ctx, (*[]byte)(&raw)); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("get code: %w", err)
	}
	if len(raw) == 0 {
		return nil, shared.ErrCodeInvalid
	}
	access, err := shared.UnmarshalCodeAccess(raw)
	if err != nil {
		return nil, err
	}
	if err := shared.GetCodeLive(access, time.Now()); err != nil {
		return nil, err
	}
	return access, nil
}

func PutCodeUsed(ctx context.Context, appID uuid.UUID, hash string) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := instance.NewRaw("SELECT to_jsonb(c) FROM claimius.update_code_used(?, ?) c", appID, hash).Scan(ctx, (*[]byte)(&raw)); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, shared.ErrCodeInvalid
	}
	return raw, nil
}

func PostCodeRedeem(ctx context.Context, appID uuid.UUID, hash string, userID uuid.UUID) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := instance.NewRaw("SELECT claimius.redeem_code(?, ?, ?)", uuid.NullUUID{UUID: appID, Valid: appID != uuid.Nil}, hash, userID).Scan(ctx, (*[]byte)(&raw)); err != nil {
		var pgErr pgdriver.Error
		if errors.As(err, &pgErr) && (pgErr.Field('C') == "P0404" || pgErr.Field('C') == "P0403") {
			return nil, shared.ErrCodeInvalid
		}
		return nil, err
	}
	return raw, nil
}

func PutClaimGuest(ctx context.Context, appID, userID, claimID uuid.UUID, mode shared.GuestMode) (json.RawMessage, error) {
	if mode != shared.GUEST_MODE_OPEN && mode != shared.GUEST_MODE_CODE && mode != shared.GUEST_MODE_OFF {
		return nil, fmt.Errorf("%w: mode must be open, code or off", shared.ErrBadCode)
	}
	var raw json.RawMessage
	if err := instance.NewRaw("SELECT claimius.update_claim_guest(?, ?, ?, ?, ?)",
		appID, claimID, userID, GetGuestUserID(appID, claimID), string(mode),
	).Scan(ctx, (*[]byte)(&raw)); err != nil {
		return nil, codeError(err)
	}
	return raw, nil
}

func GetGuestUserID(appID, claimID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(appID, []byte("guest:"+claimID.String()))
}
