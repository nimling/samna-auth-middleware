package db

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"errors"

	"github.com/google/uuid"
)

func (d *DB) GetSecrets(ctx context.Context, userID uuid.UUID, accessLevel ClaimAccessLevel) ([]SamnaSecret, error) {
	var secrets []SamnaSecret
	err := d.NewRaw("SELECT * FROM samna_auth.get_secrets(?, ?)", userID, accessLevel).
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
		Limit(1).
		Scan(ctx)
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

	var app SamnaApp
	err = d.NewSelect().
		Model(&app).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx)
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

	block, err := aes.NewCipher(seed)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	plaintext, err := gcm.Open(nil, ciphertext[:nonceSize], ciphertext[nonceSize:], nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
