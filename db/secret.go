package db

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

func (d *DB) GetSecrets(ctx context.Context, userID uuid.UUID, requiredAccess ClaimAccess) ([]SamnaSecret, error) {
	secrets := make([]SamnaSecret, 0)
	err := d.NewRaw("SELECT * FROM claimius.get_secrets(?, claimius.get_disciple_app_id(), ?)", userID, requiredAccess).
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
		ExcludeColumn("app_id").
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &secret, nil
}

func (d *DB) insertSecret(ctx context.Context, ac *AccessClaim, body BodySecret) (*SamnaSecret, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	if ac.ClaimID == nil {
		return nil, ErrNoAccess
	}
	if body.Key == nil || *body.Key == "" {
		return nil, fmt.Errorf("key is required")
	}
	if body.Value == nil || *body.Value == "" {
		return nil, fmt.Errorf("value is required")
	}
	if body.OwnerID == nil || *body.OwnerID == uuid.Nil {
		return nil, fmt.Errorf("owner_id is required")
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
	secret := SamnaSecret{
		Key:       *body.Key,
		Value:     hex.EncodeToString(encrypted),
		ExpiresAt: body.ExpiresAt,
		OwnerID:   *body.OwnerID,
		CreatedBy: ac.UserClaimID,
	}
	err = d.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().
			Model(&secret).
			ExcludeColumn("id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
			Value("app_id", "claimius.get_disciple_app_id()").
			Returning("*").
			Exec(ctx, &secret); err != nil {
			return fmt.Errorf("insert secret: %w", err)
		}
		if _, err := tx.NewRaw(`
			SELECT claimius.assign_claim_object(
				p_app_id        := claimius.get_disciple_app_id(),
				p_claim_id      := ?,
				p_sa_owner_id   := ?,
				p_sa_root_id    := ?,
				p_sa_created_by := ?,
				p_object_id     := ?,
				p_object_type   := 'claimius.samna_secret',
				p_sa_access     := ?,
				p_inherits      := TRUE
			)
		`, *ac.ClaimID, ac.OwnerID, ac.RootID, *ac.UserClaimID, secret.ID, int(ACCESS_OWNER|ACCESS_WRITE|ACCESS_READ)).Exec(ctx); err != nil {
			return fmt.Errorf("assign claim object: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &secret, nil
}

func (d *DB) updateSecret(ctx context.Context, ac *AccessClaim, secretID uuid.UUID, body BodySecret) (*SamnaSecret, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	secret, err := d.GetSecret(ctx, secretID)
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
	_, err = d.DB.NewUpdate().
		Model(secret).
		Where("sa_deleted_at IS NULL").
		Where("id = ?", secretID).
		OmitZero().
		ExcludeColumn("app_id").
		Returning("*").
		Exec(ctx, secret)
	if err != nil {
		return nil, fmt.Errorf("update secret: %w", err)
	}
	return secret, nil
}

func (d *DB) deleteSecret(ctx context.Context, ac *AccessClaim, secretID uuid.UUID) (*SamnaSecret, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	var secret SamnaSecret
	_, err := d.DB.NewUpdate().
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

func (d *DB) ResolveSecret(ctx context.Context, secretID uuid.UUID) (string, error) {
	secret, err := d.GetSecret(ctx, secretID)
	if err != nil {
		return "", err
	}
	if secret.ExpiresAt != nil && !secret.ExpiresAt.After(time.Now()) {
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
	ciphertext, err := hex.DecodeString(secret.Value)
	if err != nil {
		return "", err
	}
	return aesDecrypt(ciphertext, seed)
}
