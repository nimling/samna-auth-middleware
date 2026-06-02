package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
)

var (
	ErrNoAccess       = errors.New("no access")
	ErrInternalServer = errors.New("internal server error")
	ErrRootConflict   = errors.New("identity already registered under a different root")
)

func selfAccessClaim(userID uuid.UUID, requiredAccess ClaimAccess) *AccessClaim {
	return &AccessClaim{
		UserObjectID: userID,
		Access:       int(requiredAccess),
		OwnerID:      userID,
		RootID:       userID,
		DirectGrant:  true,
	}
}

func (d *DB) MutationAccess(ctx context.Context, userID, objectID uuid.UUID, objectType string, requiredAccess ClaimAccess) (*AccessClaim, error) {
	uc, err := d.GetAccess(ctx, userID, objectID, objectType, requiredAccess)
	if err != nil && !errors.Is(err, ErrNoAccess) {
		return nil, err
	}
	if uc != nil {
		return uc, nil
	}
	uc, err = d.GetDirectAccess(ctx, userID, objectID, objectType)
	if err != nil && !errors.Is(err, ErrNoAccess) {
		return nil, err
	}
	if uc == nil {
		return nil, ErrNoAccess
	}
	if (uc.Access & int(requiredAccess)) != int(requiredAccess) {
		return nil, ErrNoAccess
	}
	return uc, nil
}

func (d *DB) UserAccess(ctx context.Context, actorID, targetUserID uuid.UUID, requiredAccess ClaimAccess) (*AccessUser, error) {
	users := make([]AccessUser, 0)
	err := d.NewSelect().
		TableExpr(`claimius.get_user_access(?, claimius.get_disciple_app_id(), ?, ?) AS u`,
			actorID, targetUserID, requiredAccess).
		Column("u.*").
		Scan(ctx, &users)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoAccess
		}
		return nil, ErrInternalServer
	}
	if len(users) == 0 {
		return nil, ErrNoAccess
	}
	return &users[0], nil
}

func (d *DB) RootAccess(ctx context.Context, userID uuid.UUID, requiredAccess ClaimAccess) (*AccessClaim, error) {
	claims := make([]AccessClaim, 0)
	err := d.NewSelect().
		TableExpr(`claimius.get_access(
			?,
			claimius.get_disciple_app_id(),
			(
				SELECT DISTINCT c.sa_root_id
				FROM claimius.user_claim uc
				JOIN claimius.claim c ON c.id = uc.claim_id
				WHERE uc.user_id = ?
				  AND uc.app_id = claimius.get_disciple_app_id()
				  AND uc.sa_deleted_at IS NULL
				  AND c.sa_deleted_at IS NULL
				LIMIT 1
			),
			?,
			?
		) AS claim`, userID, userID, OBJECT_TYPE_ORGANIZATION, requiredAccess).
		Column("claim.*").
		Scan(ctx, &claims)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoAccess
		}
		return nil, ErrInternalServer
	}
	if len(claims) == 0 {
		return nil, ErrNoAccess
	}
	return &claims[0], nil
}
