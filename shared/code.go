package shared

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/v2/client"
	"github.com/nimling/samna-auth-middleware/v2/sauth"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"
)

const CodeCookie = "__Host-sauth_samk"

type CodeKind string

const (
	CODE_KIND_INVITE CodeKind = "invite"
	CODE_KIND_GUEST  CodeKind = "guest"
)

type GuestMode string

const (
	GUEST_MODE_OPEN GuestMode = "open"
	GUEST_MODE_CODE GuestMode = "code"
	GUEST_MODE_OFF  GuestMode = "off"
)

var ErrCodeInvalid = errors.New("code is unknown, revoked, outside its window or used up")

var AnonymousUserID = uuid.NewSHA1(uuid.NameSpaceURL, []byte("claimius:anonymous"))

type Code struct {
	bun.BaseModel `bun:"table:claimius.code"`

	ID         uuid.UUID       `bun:"id,pk,default:gen_random_uuid()" json:"id"`
	Hash       string          `bun:"hash" json:"-" sauth:"hash"`
	Kind       CodeKind        `bun:"kind" json:"kind"`
	ClaimID    uuid.UUID       `bun:"claim_id" json:"claim_id"`
	ObjectID   *uuid.UUID      `bun:"object_id" json:"object_id,omitempty"`
	ObjectType *string         `bun:"object_type" json:"object_type,omitempty"`
	Data       json.RawMessage `bun:"data,type:jsonb" json:"data"`
	StartsAt   *time.Time      `bun:"starts_at" json:"starts_at,omitempty"`
	EndsAt     *time.Time      `bun:"ends_at" json:"ends_at,omitempty"`
	Count      int             `bun:"count" json:"count"`
	UsedCount  int             `bun:"used_count" json:"used_count"`
	RevokedAt  *time.Time      `bun:"revoked_at" json:"revoked_at,omitempty"`
	OwnerID    uuid.UUID       `bun:"sa_owner_id" json:"owner_id"`
	CreatedBy  uuid.UUID       `bun:"sa_created_by" json:"created_by"`
	DeletedAt  *time.Time      `bun:"sa_deleted_at" json:"deleted_at,omitzero"`
	CreatedAt  time.Time       `bun:"sa_created_at,default:now()" json:"created_at"`
	UpdatedAt  time.Time       `bun:"sa_updated_at,default:now()" json:"updated_at"`
}

type CodeAccess struct {
	Code
	AppID            *uuid.UUID `bun:"-" json:"app_id,omitempty"`
	GuestMode        GuestMode  `bun:"-" json:"guest_mode"`
	GuestUserID      *uuid.UUID `bun:"-" json:"guest_user_id,omitempty"`
	GuestUserClaimID *uuid.UUID `bun:"-" json:"guest_user_claim_id,omitempty"`
}

type BodyCode struct {
	Kind       CodeKind        `json:"kind"`
	ObjectID   *uuid.UUID      `json:"object_id,omitempty"`
	ObjectType *string         `json:"object_type,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
	StartsAt   *time.Time      `json:"starts_at,omitempty"`
	EndsAt     *time.Time      `json:"ends_at,omitempty"`
	Count      int             `json:"count"`
	Path       string          `json:"path,omitempty"`
}

type BodySamCode struct {
	BodyCode
	ClaimID uuid.UUID `json:"claim_id"`
	UserID  uuid.UUID `json:"user_id"`
	Hash    string    `json:"hash"`
}

type BodySamCodeHash struct {
	Hash   string    `json:"hash"`
	UserID uuid.UUID `json:"user_id,omitempty"`
}

type BodySamCodeActor struct {
	UserID uuid.UUID `json:"user_id"`
}

type BodyClaimGuest struct {
	Mode GuestMode `json:"mode"`
}

type BodySamClaimGuest struct {
	UserID uuid.UUID `json:"user_id"`
	Mode   GuestMode `json:"mode"`
}

type CodeCreated struct {
	Code
	Value string `bun:"-" json:"value"`
	URL   string `bun:"-" json:"url,omitempty"`
}

type CodeRedeemed struct {
	Code      Code      `json:"code"`
	UserClaim UserClaim `json:"user_claim"`
}

type ClaimGuest struct {
	Claim     Claim     `json:"claim"`
	SamnaUser SamnaUser `json:"samna_user"`
	UserClaim UserClaim `json:"user_claim"`
}

func GetCodeValue() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("code value: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func GetCodeHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func GetCodeLive(ca *CodeAccess, now time.Time) error {
	if ca == nil || ca.RevokedAt != nil || ca.DeletedAt != nil {
		return ErrCodeInvalid
	}
	if ca.StartsAt != nil && now.Before(*ca.StartsAt) {
		return ErrCodeInvalid
	}
	if ca.EndsAt != nil && !now.Before(*ca.EndsAt) {
		return ErrCodeInvalid
	}
	if ca.Count > 0 && ca.UsedCount >= ca.Count {
		return ErrCodeInvalid
	}
	if ca.Kind == CODE_KIND_GUEST && (ca.GuestMode == GUEST_MODE_OFF || ca.GuestUserID == nil) {
		return ErrCodeInvalid
	}
	return nil
}

type cachedCode struct {
	access *CodeAccess
	at     time.Time
}

const codeCacheTTL = 30 * time.Second

var codeCache sync.Map

func DeleteCodeCache() {
	codeCache.Clear()
}

func UnmarshalCodeAccess(raw []byte) (*CodeAccess, error) {
	var ca CodeAccess
	if err := sauth.Unmarshal(raw, &ca); err != nil {
		return nil, err
	}
	return &ca, nil
}

func (d *DB) GetCodeAccess(ctx context.Context, value string) (*CodeAccess, error) {
	hash := GetCodeHash(value)
	now := time.Now()
	if v, ok := codeCache.Load(hash); ok {
		c := v.(cachedCode)
		if now.Sub(c.at) < codeCacheTTL {
			if err := GetCodeLive(c.access, now); err != nil {
				return nil, err
			}
			return c.access, nil
		}
	}
	var raw []byte
	if err := d.NewRaw("SELECT claimius.get_code(?)", hash).Scan(ctx, (*[]byte)(&raw)); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("get code: %w", err)
	}
	if len(raw) == 0 {
		return nil, ErrCodeInvalid
	}
	ca, err := UnmarshalCodeAccess(raw)
	if err != nil {
		return nil, fmt.Errorf("read code: %w", err)
	}
	codeCache.Store(hash, cachedCode{access: ca, at: now})
	if err := GetCodeLive(ca, now); err != nil {
		return nil, err
	}
	return ca, nil
}

func (d *DB) GetCodes(ctx context.Context, claimID uuid.UUID) ([]Code, error) {
	codes := make([]Code, 0)
	if err := d.NewRaw("SELECT * FROM claimius.get_codes(?)", claimID).Scan(ctx, &codes); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return codes, nil
}

func codeError(err error) error {
	var pgErr pgdriver.Error
	if errors.As(err, &pgErr) {
		switch pgErr.Field('C') {
		case "P0404":
			return fmt.Errorf("%w: %w", ErrNotFound, err)
		case "P0403":
			return fmt.Errorf("%w: %w", ErrNoAccess, err)
		case "22023":
			return fmt.Errorf("%w: %s", ErrBadCode, pgErr.Field('M'))
		}
	}
	switch {
	case errors.Is(err, client.ErrNotFound):
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	case errors.Is(err, client.ErrForbidden):
		return fmt.Errorf("%w: %w", ErrNoAccess, err)
	case errors.Is(err, client.ErrBadRequest):
		return fmt.Errorf("%w: %w", ErrBadCode, err)
	}
	return err
}

var ErrBadCode = errors.New("bad code request")

type replayEvent struct {
	eventType string
	raw       json.RawMessage
}

func (d *DB) applyReplayed(ctx context.Context, events ...replayEvent) error {
	return d.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, "SELECT set_config('claimius.replay_mode', 'true', true)"); err != nil {
			return err
		}
		for _, event := range events {
			if err := ApplyEvent(ctx, d.DB, tx, "upsert", event.eventType, event.raw); err != nil {
				return fmt.Errorf("apply %s: %w", event.eventType, err)
			}
		}
		_, err := tx.ExecContext(ctx, "SELECT set_config('claimius.replay_mode', 'false', true)")
		return err
	})
}

func (d *DB) applyCode(ctx context.Context, raw json.RawMessage) (*Code, error) {
	if err := d.applyReplayed(ctx, replayEvent{OBJECT_TYPE_CODE, raw}); err != nil {
		return nil, err
	}
	var code Code
	if err := sauth.Unmarshal(raw, &code); err != nil {
		return nil, err
	}
	return &code, nil
}

func (d *DB) PostCode(ctx context.Context, ac *AccessClaim, claimID uuid.UUID, body BodyCode, base string) (*CodeCreated, error) {
	if ac == nil || ac.UserID == uuid.Nil {
		return nil, ErrNoAccess
	}
	if body.Kind != CODE_KIND_INVITE && body.Kind != CODE_KIND_GUEST {
		return nil, fmt.Errorf("%w: kind must be invite or guest", ErrBadCode)
	}
	if body.Count < 0 {
		return nil, fmt.Errorf("%w: count must not be negative", ErrBadCode)
	}
	value, err := GetCodeValue()
	if err != nil {
		return nil, err
	}
	hash := GetCodeHash(value)
	var code *Code
	if d.RouteToProphet {
		var raw json.RawMessage
		payload := BodySamCode{BodyCode: body, ClaimID: claimID, UserID: ac.UserID, Hash: hash}
		if err := client.Post(ctx, "code", payload, &raw); err != nil {
			return nil, codeError(err)
		}
		if code, err = d.applyCode(ctx, raw); err != nil {
			return nil, err
		}
	} else {
		var data any
		if len(body.Data) > 0 {
			data = string(body.Data)
		}
		var raw json.RawMessage
		if err := d.NewRaw("SELECT claimius.insert_code(?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?) -> 'code'",
			ac.UserID, claimID, hash, string(body.Kind), body.ObjectID, body.ObjectType, data, body.StartsAt, body.EndsAt, body.Count,
		).Scan(ctx, (*[]byte)(&raw)); err != nil {
			return nil, codeError(err)
		}
		code = &Code{}
		if err := sauth.Unmarshal(raw, code); err != nil {
			return nil, err
		}
	}
	created := &CodeCreated{Code: *code, Value: value}
	if base != "" {
		if created.URL, err = client.GetCodeURL(base, body.Path, value); err != nil {
			return nil, err
		}
	}
	return created, nil
}

func (d *DB) DeleteCode(ctx context.Context, ac *AccessClaim, codeID uuid.UUID) (*Code, error) {
	if ac == nil || ac.UserID == uuid.Nil {
		return nil, ErrNoAccess
	}
	defer DeleteCodeCache()
	if d.RouteToProphet {
		var raw json.RawMessage
		if err := client.Delete(ctx, "code/"+codeID.String(), BodySamCodeActor{UserID: ac.UserID}, &raw); err != nil {
			return nil, codeError(err)
		}
		return d.applyCode(ctx, raw)
	}
	var raw json.RawMessage
	if err := d.NewRaw("SELECT claimius.delete_code(?, ?) -> 'code'", ac.UserID, codeID).Scan(ctx, (*[]byte)(&raw)); err != nil {
		return nil, codeError(err)
	}
	var code Code
	if err := sauth.Unmarshal(raw, &code); err != nil {
		return nil, err
	}
	return &code, nil
}

func (d *DB) PutCodeUsed(ctx context.Context, value string) (*Code, error) {
	hash := GetCodeHash(value)
	defer codeCache.Delete(hash)
	if d.RouteToProphet {
		var raw json.RawMessage
		if err := client.Put(ctx, "code/used", BodySamCodeHash{Hash: hash}, &raw); err != nil {
			if errors.Is(err, client.ErrNotFound) {
				return nil, ErrCodeInvalid
			}
			return nil, codeError(err)
		}
		return d.applyCode(ctx, raw)
	}
	codes := make([]Code, 0, 1)
	if err := d.NewRaw("SELECT * FROM claimius.update_code_used(?)", hash).Scan(ctx, &codes); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if len(codes) == 0 {
		return nil, ErrCodeInvalid
	}
	return &codes[0], nil
}

func (d *DB) PostCodeRedeem(ctx context.Context, value string, userID uuid.UUID) (*UserClaim, error) {
	hash := GetCodeHash(value)
	defer codeCache.Delete(hash)
	var raw json.RawMessage
	if d.RouteToProphet {
		if err := client.Post(ctx, "code/redeem", BodySamCodeHash{Hash: hash, UserID: userID}, &raw); err != nil {
			if errors.Is(err, client.ErrNotFound) || errors.Is(err, client.ErrForbidden) {
				return nil, ErrCodeInvalid
			}
			return nil, codeError(err)
		}
	} else if err := d.NewRaw("SELECT claimius.redeem_code(?, ?)", hash, userID).Scan(ctx, (*[]byte)(&raw)); err != nil {
		var pgErr pgdriver.Error
		if errors.As(err, &pgErr) && (pgErr.Field('C') == "P0404" || pgErr.Field('C') == "P0403") {
			return nil, ErrCodeInvalid
		}
		return nil, err
	}
	var parts struct {
		Code      json.RawMessage `json:"code"`
		UserClaim json.RawMessage `json:"user_claim"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, err
	}
	if d.RouteToProphet {
		if err := d.applyReplayed(ctx, replayEvent{OBJECT_TYPE_CODE, parts.Code}, replayEvent{OBJECT_TYPE_USER_CLAIM, parts.UserClaim}); err != nil {
			return nil, err
		}
	}
	var grant UserClaim
	if err := sauth.Unmarshal(parts.UserClaim, &grant); err != nil {
		return nil, err
	}
	return &grant, nil
}

func (d *DB) PutClaimGuest(ctx context.Context, ac *AccessClaim, claimID uuid.UUID, mode GuestMode) (*Claim, error) {
	if ac == nil || ac.UserID == uuid.Nil {
		return nil, ErrNoAccess
	}
	if mode != GUEST_MODE_OPEN && mode != GUEST_MODE_CODE && mode != GUEST_MODE_OFF {
		return nil, fmt.Errorf("%w: mode must be open, code or off", ErrBadCode)
	}
	defer DeleteCodeCache()
	var raw json.RawMessage
	if d.RouteToProphet {
		if err := client.Put(ctx, "claim/"+claimID.String()+"/guest", BodySamClaimGuest{UserID: ac.UserID, Mode: mode}, &raw); err != nil {
			return nil, codeError(err)
		}
	} else if err := d.NewRaw("SELECT claimius.update_claim_guest(?, ?, ?)", claimID, ac.UserID, string(mode)).Scan(ctx, (*[]byte)(&raw)); err != nil {
		return nil, codeError(err)
	}
	var parts struct {
		Claim     json.RawMessage `json:"claim"`
		SamnaUser json.RawMessage `json:"samna_user"`
		UserClaim json.RawMessage `json:"user_claim"`
	}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, err
	}
	if d.RouteToProphet {
		if err := d.applyReplayed(ctx, replayEvent{OBJECT_TYPE_USER, parts.SamnaUser}, replayEvent{OBJECT_TYPE_USER_CLAIM, parts.UserClaim}, replayEvent{OBJECT_TYPE_CLAIM, parts.Claim}); err != nil {
			return nil, err
		}
	}
	var claim Claim
	if err := sauth.Unmarshal(parts.Claim, &claim); err != nil {
		return nil, err
	}
	return &claim, nil
}

func (d *DB) getGuestAccess(ctx context.Context, objectID uuid.UUID, objectType string, requiredAccess ClaimAccess) (*AccessClaim, error) {
	if requiredAccess&^ACCESS_READ != 0 {
		return nil, ErrNoAccess
	}
	type guestAccessRow struct {
		GuestUserID uuid.UUID `bun:"user_id"`
		AccessClaim
	}
	rows := make([]guestAccessRow, 0, 1)
	if err := d.NewRaw("SELECT * FROM claimius.get_guest_access(?, ?)", objectType, objectID).Scan(ctx, &rows); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInternalServer
	}
	if len(rows) == 0 {
		return nil, ErrNoAccess
	}
	ac := rows[0].AccessClaim
	ac.UserID = rows[0].GuestUserID
	claimID := uuid.Nil
	if ac.ClaimID != nil {
		claimID = *ac.ClaimID
	}
	if err := WriteAudit(ctx, d.DB, nil, "guest request", "read", objectType, objectID, ac.OwnerID, ac.UserClaimID,
		fmt.Sprintf("open guest of claim %s", claimID)); err != nil {
		return nil, ErrInternalServer
	}
	return &ac, nil
}
