package db

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
)

type BodySecret struct {
	Key   *string `json:"key"`
	Value *string `json:"value"`
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

func (d *DB) PostSecret(ctx context.Context, body BodySecret) (*SamnaSecret, error) {
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
		Key:     *body.Key,
		Value:   hex.EncodeToString(encrypted),
		OwnerID: app.OwnerID,
	}
	_, err = d.NewInsert().
		Model(&secret).
		ExcludeColumn("id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		Value("app_id", "claimius.get_disciple_app_id()").
		Returning("*").
		Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("insert secret: %w", err)
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
