package db

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/utils"
)

func (d *DB) getAppSeed(ctx context.Context) (uuid.UUID, []byte, error) {
	slug := os.Getenv("SAUTH_APP_SLUG")

	var app SamnaApp
	err := d.NewSelect().
		Model(&app).
		Where("slug = ?", slug).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx)
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("get app: %w", err)
	}

	seed, err := hex.DecodeString(app.PrivateSeed)
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("decode seed: %w", err)
	}

	return app.ID, seed, nil
}

func (d *DB) decryptSecret(secret *SamnaSecret, seed []byte) error {
	ciphertext, err := hex.DecodeString(secret.ValueEncrypted)
	if err != nil {
		return fmt.Errorf("decode ciphertext: %w", err)
	}

	value, err := utils.AesDecrypt(ciphertext, seed)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}

	secret.Value = value
	secret.ValueEncrypted = ""
	return nil
}

func (d *DB) GetSecrets(ctx context.Context) ([]SamnaSecret, error) {
	appID, seed, err := d.getAppSeed(ctx)
	if err != nil {
		return nil, err
	}

	var secrets []SamnaSecret
	err = d.NewSelect().
		Model(&secrets).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	for i := range secrets {
		if err := d.decryptSecret(&secrets[i], seed); err != nil {
			return nil, err
		}
	}

	return secrets, nil
}

func (d *DB) GetSecret(ctx context.Context, secretID uuid.UUID) (*SamnaSecret, error) {
	appID, seed, err := d.getAppSeed(ctx)
	if err != nil {
		return nil, err
	}

	var secret SamnaSecret
	err = d.NewSelect().
		Model(&secret).
		Where("id = ?", secretID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	if err := d.decryptSecret(&secret, seed); err != nil {
		return nil, err
	}

	return &secret, nil
}

func (d *DB) ResolveSecret(ctx context.Context, secretID uuid.UUID) (string, error) {
	secret, err := d.GetSecret(ctx, secretID)
	if err != nil {
		return "", err
	}
	return secret.Value, nil
}
