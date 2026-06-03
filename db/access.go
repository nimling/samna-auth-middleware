package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/lib/pq"
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

type AccessRequest struct {
	ObjectID       uuid.UUID
	ObjectType     string
	RequiredAccess ClaimAccess
}

func (d *DB) MutationAccessMany(ctx context.Context, userID uuid.UUID, requests []AccessRequest) ([]*AccessClaim, error) {
	if len(requests) == 0 {
		return nil, nil
	}
	objectIDs := make([]uuid.UUID, len(requests))
	objectTypes := make([]string, len(requests))
	requiredAccesses := make([]int64, len(requests))
	for i, r := range requests {
		objectIDs[i] = r.ObjectID
		objectTypes[i] = r.ObjectType
		requiredAccesses[i] = int64(r.RequiredAccess)
	}
	type batchRow struct {
		Idx int `bun:"idx"`
		AccessClaim
	}
	rows := make([]batchRow, 0, len(requests))
	err := d.NewRaw(`
		WITH inputs AS (
			SELECT object_id, object_type, required_access,
			       row_number() OVER () AS idx
			FROM unnest(?::uuid[], ?::text[], ?::int[]) AS u(object_id, object_type, required_access)
		)
		SELECT
			inputs.idx,
			g.user_object_id,
			g.user_claim_id,
			g.claim_id,
			g.sa_access,
			g.scope,
			g.sa_owner_id,
			g.sa_root_id,
			g.sa_location_id,
			g.direct_grant
		FROM inputs
		LEFT JOIN LATERAL claimius.get_access(?, claimius.get_disciple_app_id(), inputs.object_id, inputs.object_type, inputs.required_access) g ON TRUE
		ORDER BY inputs.idx
	`, pq.Array(objectIDs), pq.Array(objectTypes), pq.Array(requiredAccesses), userID).Scan(ctx, &rows)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return make([]*AccessClaim, len(requests)), nil
		}
		return nil, ErrInternalServer
	}
	result := make([]*AccessClaim, len(requests))
	for i := range rows {
		idx := rows[i].Idx - 1
		if idx < 0 || idx >= len(requests) {
			continue
		}
		if rows[i].UserObjectID == uuid.Nil {
			continue
		}
		ac := rows[i].AccessClaim
		result[idx] = &ac
	}
	return result, nil
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
