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

func (d *DB) GetSecrets(ctx context.Context, userID uuid.UUID, accessLevel ClaimAccessLevel) ([]SamnaSecret, error) {
	secrets := make([]SamnaSecret, 0)
	err := d.NewRaw("SELECT * FROM claimius.get_secrets(?, claimius.get_disciple_app_id(), ?)", userID, accessLevel).
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

func (d *DB) PostSecret(ctx context.Context, body BodySecret, ownerID, createdBy, rootID, claimID uuid.UUID) (*SamnaSecret, error) {
	if body.Key == nil || *body.Key == "" {
		return nil, fmt.Errorf("key is required")
	}
	if body.Value == nil || *body.Value == "" {
		return nil, fmt.Errorf("value is required")
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
		OwnerID:   ownerID,
		CreatedBy: &createdBy,
	}
	err = d.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().
			Model(&secret).
			ExcludeColumn("id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
			Value("app_id", "claimius.get_disciple_app_id()").
			Returning("*").
			Exec(ctx); err != nil {
			return fmt.Errorf("insert secret: %w", err)
		}
		if _, err := tx.NewRaw(`
			SELECT claimius.assign_claim_object(
				claimius.get_disciple_app_id(),
				?, ?, 'claimius.samna_secret', ?, ?, ?, ?, TRUE
			)
		`, claimID, secret.ID, ownerID, rootID, createdBy, int(ACCESS_LEVEL_ADMIN)).Exec(ctx); err != nil {
			return fmt.Errorf("assign claim object: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &secret, nil
}

func (d *DB) PutSecret(ctx context.Context, secretID uuid.UUID, body BodySecret) (*SamnaSecret, error) {
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
	_, err = d.NewUpdate().
		Model(secret).
		Where("id = ?", secretID).
		OmitZero().
		ExcludeColumn("app_id").
		Returning("*").
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("update secret: %w", err)
	}
	return secret, nil
}

func (d *DB) DeleteSecret(ctx context.Context, secretID uuid.UUID) error {
	_, err := d.NewUpdate().
		Model((*SamnaSecret)(nil)).
		Where("id = ?", secretID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Exec(ctx)
	return err
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
