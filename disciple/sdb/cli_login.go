package sdb

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type CliLogin struct {
	bun.BaseModel  `bun:"table:claimius.cli_login"`
	ID             uuid.UUID      `bun:"id,pk,type:uuid,default:gen_random_uuid()"`
	DeviceCodeHash string         `bun:"device_code_hash"`
	UserCodeHash   string         `bun:"user_code_hash"`
	HandleHash     string         `bun:"handle_hash"`
	Approved       bool           `bun:"approved"`
	RefreshToken   sql.NullString `bun:"refresh_token"`
	ExpiresAt      time.Time      `bun:"expires_at"`
}

func (d *DB) DeleteExpiredCliLogins(ctx context.Context) error {
	_, err := d.DB.DB.NewDelete().Model((*CliLogin)(nil)).Where("expires_at <= now()").Exec(ctx)
	return err
}

func (d *DB) PostCliLogin(ctx context.Context, deviceCodeHash string, userCodeHash string, handleHash string, expiresAt time.Time) (uuid.UUID, error) {
	login := CliLogin{DeviceCodeHash: deviceCodeHash, UserCodeHash: userCodeHash, HandleHash: handleHash, ExpiresAt: expiresAt}
	_, err := d.DB.DB.NewInsert().Model(&login).ExcludeColumn("id", "approved", "refresh_token").Returning("id").Exec(ctx)
	return login.ID, err
}

func (d *DB) GetCliLoginOpen(ctx context.Context, handleHash string) (bool, error) {
	return d.DB.DB.NewSelect().Model((*CliLogin)(nil)).
		Where("handle_hash = ?", handleHash).
		Where("NOT approved").
		Where("expires_at > now()").
		Exists(ctx)
}

func (d *DB) GetCliLogin(ctx context.Context, id uuid.UUID) (*CliLogin, error) {
	var login CliLogin
	err := d.DB.DB.NewSelect().Model(&login).
		Column("id", "device_code_hash", "approved").
		Where("id = ?", id).
		Where("expires_at > now()").
		Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &login, nil
}

func (d *DB) PutCliLoginApproval(ctx context.Context, handleHash string, userCodeHash string, refreshToken string) (bool, error) {
	res, err := d.DB.DB.NewUpdate().Model((*CliLogin)(nil)).
		Set("approved = TRUE").
		Set("refresh_token = ?", refreshToken).
		Where("handle_hash = ?", handleHash).
		Where("user_code_hash = ?", userCodeHash).
		Where("NOT approved").
		Where("expires_at > now()").
		Exec(ctx)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	return rows > 0, err
}

func (d *DB) DeleteCliLogin(ctx context.Context, id uuid.UUID, deviceCodeHash string) (string, bool, error) {
	var login CliLogin
	err := d.DB.DB.NewDelete().Model(&login).
		Where("id = ?", id).
		Where("device_code_hash = ?", deviceCodeHash).
		Where("approved").
		Where("expires_at > now()").
		Returning("refresh_token").
		Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return login.RefreshToken.String, true, nil
}
