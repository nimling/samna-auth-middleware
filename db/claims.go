package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (d *DB) CreateClaim(ctx context.Context, name string, description string, accessLevel int, ownerID uuid.UUID, rootID uuid.UUID, createdBy uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := d.NewRaw(
		"SELECT samna_auth.create_claim(?, ?, ?, ?, ?, ?)",
		name, description, accessLevel, ownerID, rootID, createdBy,
	).Scan(ctx, &id)
	return id, err
}

func (d *DB) CreateSystemClaim(ctx context.Context, name string, description string, accessLevel int, ownerID uuid.UUID, rootID uuid.UUID, createdBy uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := d.NewRaw(
		"SELECT samna_auth.create_system_claim(?, ?, ?, ?, ?, ?)",
		name, description, accessLevel, ownerID, rootID, createdBy,
	).Scan(ctx, &id)
	return id, err
}

func (d *DB) AssignClaimToUser(ctx context.Context, claimID uuid.UUID, userID uuid.UUID, ownerID uuid.UUID, createdBy uuid.UUID, reason *string, startsAt *time.Time, endsAt *time.Time) (uuid.UUID, error) {
	var id uuid.UUID
	err := d.NewRaw(
		"SELECT samna_auth.assign_claim_user(?, ?, ?, ?, ?, ?, ?)",
		claimID, userID, ownerID, createdBy, reason, startsAt, endsAt,
	).Scan(ctx, &id)
	return id, err
}

func (d *DB) AssignClaimToObject(ctx context.Context, claimID uuid.UUID, objectID uuid.UUID, objectType string, ownerID uuid.UUID, rootID uuid.UUID, createdBy uuid.UUID, level *int, inherits *bool, reason *string, refID *uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	err := d.NewRaw(
		"SELECT samna_auth.assign_claim_object(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		claimID, objectID, objectType, ownerID, rootID, createdBy, level, inherits, reason, refID,
	).Scan(ctx, &id)
	return id, err
}

func (d *DB) RemoveUserClaim(ctx context.Context, userClaimID uuid.UUID, deletedBy uuid.UUID) (bool, error) {
	var result bool
	err := d.NewRaw(
		"SELECT samna_auth.remove_user_claim(?, ?)",
		userClaimID, deletedBy,
	).Scan(ctx, &result)
	return result, err
}

func (d *DB) RemoveClaimObject(ctx context.Context, claimObjectID uuid.UUID, deletedBy uuid.UUID) (bool, error) {
	var result bool
	err := d.NewRaw(
		"SELECT samna_auth.remove_claim_object(?, ?)",
		claimObjectID, deletedBy,
	).Scan(ctx, &result)
	return result, err
}

func (d *DB) RemoveClaim(ctx context.Context, claimID uuid.UUID, deletedBy uuid.UUID) (bool, error) {
	var result bool
	err := d.NewRaw(
		"SELECT samna_auth.remove_claim(?, ?)",
		claimID, deletedBy,
	).Scan(ctx, &result)
	return result, err
}

func (d *DB) ClaimAccess(ctx context.Context, userID uuid.UUID, objectID *uuid.UUID, minAccessLevel int) ([]ActiveClaim, error) {
	var claims []ActiveClaim
	err := d.NewSelect().
		TableExpr("samna_auth.claim_access(?, ?, ?) AS claim", userID, objectID, minAccessLevel).
		Column("claim.*").
		Scan(ctx, &claims)
	return claims, err
}

func (d *DB) UserObjectAccess(ctx context.Context, userID uuid.UUID, objectID *uuid.UUID, accessLevel ClaimAccessLevel) (*ActiveClaim, error) {
	claims, err := d.ClaimAccess(ctx, userID, objectID, int(accessLevel))
	if err != nil {
		return nil, err
	}
	if len(claims) == 0 {
		return nil, errors.New("no access found")
	}
	return &claims[0], nil
}

func (d *DB) UserObjectHasLevel(ctx context.Context, userID uuid.UUID, objectID *uuid.UUID, accessLevel ClaimAccessLevel) bool {
	uc, err := d.UserObjectAccess(ctx, userID, objectID, accessLevel)
	return err == nil && uc != nil
}

func (d *DB) InitAuthViews(ctx context.Context, tableNames ...string) error {
	placeholders := make([]string, len(tableNames))
	args := make([]interface{}, len(tableNames))
	for i, name := range tableNames {
		placeholders[i] = "?"
		args[i] = name
	}
	query := "SELECT samna_auth.init_auth_views(" + strings.Join(placeholders, ", ") + ")"
	_, err := d.NewRaw(query, args...).Exec(ctx)
	return err
}
