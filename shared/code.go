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

const CODE_KIND_INVITE CodeKind = "invite"

var ErrCodeInvalid = errors.New("code is unknown, revoked, outside its window or used up")

var AnonymousUserID = uuid.NewSHA1(uuid.NameSpaceURL, []byte("claimius:anonymous"))

type codeHashKey struct{}

func WithCodeHash(ctx context.Context, hash string) context.Context {
	if hash == "" {
		return ctx
	}
	return context.WithValue(ctx, codeHashKey{}, hash)
}

func CodeHashFrom(ctx context.Context) string {
	hash, _ := ctx.Value(codeHashKey{}).(string)
	return hash
}

type ClaimOptionsSamk struct {
	Value  string `json:"value,omitempty"`
	Hash   string `json:"hash,omitempty"`
	Query  bool   `json:"query"`
	Header bool   `json:"header"`
	Cookie bool   `json:"cookie"`
}

type ClaimOptions struct {
	Samk *ClaimOptionsSamk `json:"samk,omitempty"`
}

func (o *ClaimOptions) Seal() (string, error) {
	if o == nil || o.Samk == nil {
		return "", nil
	}
	if o.Samk.Value == "" && o.Samk.Hash == "" {
		value, err := GetCodeValue()
		if err != nil {
			return "", err
		}
		o.Samk.Value = value
	}
	value := o.Samk.Value
	if value != "" {
		o.Samk.Hash = GetCodeHash(value)
	}
	o.Samk.Value = ""
	return value, nil
}

func SealClaimOptions(raw json.RawMessage) (json.RawMessage, string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, "", nil
	}
	var options ClaimOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, "", fmt.Errorf("%w: options: %w", ErrBadCode, err)
	}
	value, err := options.Seal()
	if err != nil {
		return nil, "", err
	}
	sealed, err := json.Marshal(options)
	if err != nil {
		return nil, "", err
	}
	return sealed, value, nil
}

func ShowClaimOptions(raw json.RawMessage, value string) json.RawMessage {
	if value == "" || len(raw) == 0 {
		return raw
	}
	var options ClaimOptions
	if err := json.Unmarshal(raw, &options); err != nil || options.Samk == nil {
		return raw
	}
	options.Samk.Value = value
	shown, err := json.Marshal(options)
	if err != nil {
		return raw
	}
	return shown
}

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
	AppID *uuid.UUID `bun:"-" json:"app_id,omitempty"`
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

type BodyGuest struct {
	ClaimID   uuid.UUID `json:"claim_id"`
	UserID    uuid.UUID `json:"user_id"`
	FirstName *string   `json:"first_name,omitempty"`
	LastName  *string   `json:"last_name,omitempty"`
	Email     *string   `json:"email,omitempty"`
	Phone     *string   `json:"phone,omitempty"`
}

type GuestTokenResponse struct {
	AccessToken     string          `json:"access_token"`
	AccessExpiresIn int             `json:"access_expires_in"`
	TokenType       string          `json:"token_type"`
	UserID          uuid.UUID       `json:"user_id"`
	ClaimID         uuid.UUID       `json:"claim_id"`
	StartsAt        *time.Time      `json:"starts_at,omitempty"`
	EndsAt          *time.Time      `json:"ends_at,omitempty"`
	SamnaUser       json.RawMessage `json:"samna_user,omitempty"`
	UserClaim       json.RawMessage `json:"user_claim,omitempty"`
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
	if body.Kind != CODE_KIND_INVITE {
		return nil, fmt.Errorf("%w: kind must be invite", ErrBadCode)
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

func (d *DB) PutGuestUser(ctx context.Context, ac *AccessClaim, body BodyGuest) (*GuestTokenResponse, error) {
	if ac == nil || ac.UserID == uuid.Nil {
		return nil, ErrNoAccess
	}
	body.UserID = ac.UserID
	var resp GuestTokenResponse
	if d.RouteToProphet {
		if err := client.Post(ctx, "user/temporary", body, &resp); err != nil {
			return nil, codeError(err)
		}
		if err := d.applyReplayed(ctx, replayEvent{OBJECT_TYPE_USER, resp.SamnaUser}, replayEvent{OBJECT_TYPE_USER_CLAIM, resp.UserClaim}); err != nil {
			return nil, err
		}
		return &resp, nil
	}
	guestID := uuid.NullUUID{}
	if body.FirstName != nil || body.LastName != nil || body.Email != nil || body.Phone != nil {
		guestID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	}
	var raw json.RawMessage
	if err := d.NewRaw("SELECT claimius.upsert_guest_user(?, ?, ?, ?, ?, ?, ?)",
		body.ClaimID, ac.UserID, guestID, body.FirstName, body.LastName, body.Email, body.Phone,
	).Scan(ctx, (*[]byte)(&raw)); err != nil {
		return nil, codeError(err)
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	var user SamnaUser
	if err := sauth.Unmarshal(resp.SamnaUser, &user); err != nil {
		return nil, err
	}
	var grant UserClaim
	if err := sauth.Unmarshal(resp.UserClaim, &grant); err != nil {
		return nil, err
	}
	resp.UserID, resp.ClaimID, resp.StartsAt, resp.EndsAt = user.UserID, body.ClaimID, grant.StartsAt, grant.EndsAt
	return &resp, nil
}

func SealGrantOptions(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	options, ok := fields["options"]
	if !ok || len(options) == 0 || string(options) == "null" {
		return raw, nil
	}
	sealed, _, err := SealClaimOptions(options)
	if err != nil {
		return nil, err
	}
	if len(sealed) == 0 {
		delete(fields, "options")
	} else {
		fields["options"] = sealed
	}
	return json.Marshal(fields)
}
