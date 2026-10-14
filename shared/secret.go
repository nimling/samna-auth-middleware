package shared

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type BodySecret struct {
	Key       *string    `json:"key"`
	Value     *string    `json:"value"`
	OwnerID   *uuid.UUID `json:"owner_id"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func (d *DB) GetSecrets(ctx context.Context, userID uuid.UUID, requiredAccess ClaimAccess, after *uuid.UUID, limit int) ([]SamnaSecret, error) {
	var pageLimit any
	if limit > 0 {
		pageLimit = limit
	}
	secrets := make([]SamnaSecret, 0)
	err := d.NewRaw("SELECT * FROM claimius.get_secrets(?, ?, ?, ?)", userID, int(requiredAccess), after, pageLimit).
		Scan(ctx, &secrets)
	if err != nil {
		return nil, err
	}
	return secrets, nil
}

func (d *DB) GetSecret(ctx context.Context, secretID uuid.UUID) (*SamnaSecret, error) {
	var secret SamnaSecret
	err := d.NewSelect().
		Model(&secret).
		ExcludeColumn("sa_deleted_at").
		Where("id = ?", secretID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &secret, nil
}

func (d *DB) insertSecret(ctx context.Context, conn bun.IConn, ac *AccessClaim, body BodySecret) (*SamnaSecret, error) {
	if ac == nil || ac.UserClaimID == nil {
		return nil, ErrNoAccess
	}
	app, err := d.GetApp(ctx)
	if err != nil {
		return nil, err
	}
	seed, err := hex.DecodeString(app.PrivateSeed)
	if err != nil {
		return nil, fmt.Errorf("decode app seed: %w", err)
	}
	encrypted, err := aesEncrypt(*body.Value, seed)
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}
	var secretID uuid.UUID
	if err := d.NewRaw(
		"SELECT (claimius.insert_secret(?, ?, ?, ?, ?) -> 'samna_secret' ->> 'id')::uuid",
		*body.Key, hex.EncodeToString(encrypted), body.ExpiresAt, *body.OwnerID, *ac.UserClaimID,
	).Conn(conn).Scan(ctx, &secretID); err != nil {
		return nil, fmt.Errorf("insert_secret: %w", err)
	}
	var secret SamnaSecret
	if err := d.NewSelect().Conn(conn).Model(&secret).ExcludeColumn("sa_deleted_at").Where("id = ?", secretID).Where("sa_deleted_at IS NULL").Limit(1).Scan(ctx); err != nil {
		return nil, err
	}
	return &secret, nil
}

func (d *DB) updateSecret(ctx context.Context, conn bun.IConn, ac *AccessClaim, secretID uuid.UUID, body BodySecret) (*SamnaSecret, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	secret := &SamnaSecret{}
	err := d.NewSelect().Conn(conn).Model(secret).ExcludeColumn("sa_deleted_at").Where("id = ?", secretID).Where("sa_deleted_at IS NULL").Limit(1).Scan(ctx)
	if err != nil {
		return nil, err
	}
	if body.Key != nil {
		secret.Key = *body.Key
	}
	if body.Value != nil {
		app, err := d.GetApp(ctx)
		if err != nil {
			return nil, err
		}
		seed, err := hex.DecodeString(app.PrivateSeed)
		if err != nil {
			return nil, fmt.Errorf("decode app seed: %w", err)
		}
		encrypted, err := aesEncrypt(*body.Value, seed)
		if err != nil {
			return nil, fmt.Errorf("encrypt: %w", err)
		}
		secret.Value = hex.EncodeToString(encrypted)
	}
	if body.ExpiresAt != nil {
		secret.ExpiresAt = body.ExpiresAt
	}
	_, err = d.DB.NewUpdate().Conn(conn).
		Model(secret).
		Where("sa_deleted_at IS NULL").
		Where("id = ?", secretID).
		OmitZero().
		Returning("*").
		Exec(ctx, secret)
	if err != nil {
		return nil, fmt.Errorf("update secret: %w", err)
	}
	return secret, nil
}

func (d *DB) deleteSecret(ctx context.Context, conn bun.IConn, ac *AccessClaim, secretID uuid.UUID) (*SamnaSecret, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	var secret SamnaSecret
	_, err := d.DB.NewUpdate().Conn(conn).
		Model(&secret).
		Where("id = ?", secretID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Returning("*").
		Exec(ctx, &secret)
	if err != nil {
		return nil, err
	}
	return &secret, nil
}

func (s *SamnaSecret) Resolve(ctx context.Context, d *DB) (string, error) {
	if s == nil {
		return "", fmt.Errorf("secret is nil")
	}
	if s.ExpiresAt != nil && !s.ExpiresAt.After(time.Now()) {
		return "", ErrSecretExpired
	}
	app, err := d.GetApp(ctx)
	if err != nil {
		return "", err
	}
	seed, err := hex.DecodeString(app.PrivateSeed)
	if err != nil {
		return "", err
	}
	ciphertext, err := hex.DecodeString(s.Value)
	if err != nil {
		return "", err
	}
	return aesDecrypt(ciphertext, seed)
}
