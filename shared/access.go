package shared

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/uptrace/bun/driver/pgdriver"
)

var (
	ErrNoAccess          = errors.New("no access")
	ErrNotFound          = errors.New("not found")
	ErrInternalServer    = errors.New("internal server error")
	ErrRootConflict      = errors.New("identity already registered under a different root")
	ErrUserAlreadyExists = errors.New("user already exists")
)

func selfAccessClaim(userID uuid.UUID, requiredAccess ClaimAccess) *AccessClaim {
	return &AccessClaim{
		UserObjectID: userID,
		Access:       requiredAccess,
		OwnerID:      userID,
		RootID:       userID,
		DirectGrant:  true,
	}
}

func (d *DB) MutationAccess(ctx context.Context, userID, objectID uuid.UUID, objectType string, requiredAccess ClaimAccess) (*AccessClaim, error) {
	if objectType == OBJECT_TYPE_USER && userID == objectID {
		return selfAccessClaim(userID, requiredAccess), nil
	}
	claims := make([]AccessClaim, 0)
	err := d.NewRaw(
		"SELECT * FROM claimius.mutation_access(?, ?, ?, ?)",
		userID, objectID, objectType, requiredAccess,
	).Scan(ctx, &claims)
	if err != nil {
		var pgErr pgdriver.Error
		if errors.As(err, &pgErr) {
			switch pgErr.Field('C') {
			case "P0404":
				return nil, ErrNotFound
			case "P0403":
				return nil, ErrNoAccess
			}
		}
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

type AccessRequest struct {
	UserID         uuid.UUID
	ObjectID       uuid.UUID
	ObjectType     string
	RequiredAccess ClaimAccess
}

type AccessClaimSetEntry struct {
	Request AccessRequest
	Claim   *AccessClaim
}

type AccessClaimSet []AccessClaimSetEntry

func (s AccessClaimSet) Get(objectType string, objectID, userID uuid.UUID) *AccessClaim {
	for _, e := range s {
		if e.Request.UserID == userID && e.Request.ObjectType == objectType && e.Request.ObjectID == objectID {
			return e.Claim
		}
	}
	return nil
}

func (s AccessClaimSet) Has(objectType string, objectID, userID uuid.UUID, required ...ClaimAccess) bool {
	claim := s.Get(objectType, objectID, userID)
	if claim == nil {
		return false
	}
	if len(required) == 0 {
		return true
	}
	mask := int(required[0])
	return (int(claim.Access) & mask) == mask
}

func (d *DB) MutationAccessMany(ctx context.Context, requests []AccessRequest) (AccessClaimSet, error) {
	if len(requests) == 0 {
		return AccessClaimSet{}, nil
	}
	userIDs := make([]uuid.UUID, len(requests))
	objectIDs := make([]uuid.UUID, len(requests))
	objectTypes := make([]string, len(requests))
	requiredAccesses := make([]int64, len(requests))
	for i, r := range requests {
		userIDs[i] = r.UserID
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
			SELECT user_id, object_id, object_type, required_access,
			       row_number() OVER () AS idx
			FROM unnest(?::uuid[], ?::uuid[], ?::text[], ?::int[]) AS u(user_id, object_id, object_type, required_access)
		)
		SELECT
			inputs.idx,
			g.user_object_id,
			g.user_claim_id,
			g.claim_id,
			g.claim_name,
			g.sa_access,
			g.scope,
			g.sa_owner_id,
			g.sa_root_id,
			g.sa_location_id,
			g.direct_grant
		FROM inputs
		LEFT JOIN LATERAL claimius.resolve_access(inputs.user_id, inputs.object_id, inputs.object_type, inputs.required_access) g ON TRUE
		ORDER BY inputs.idx
	`, pq.Array(userIDs), pq.Array(objectIDs), pq.Array(objectTypes), pq.Array(requiredAccesses)).Scan(ctx, &rows)
	set := make(AccessClaimSet, len(requests))
	for i, r := range requests {
		set[i] = AccessClaimSetEntry{Request: r}
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return set, nil
		}
		return nil, ErrInternalServer
	}
	for i := range rows {
		idx := rows[i].Idx - 1
		if idx < 0 || idx >= len(requests) {
			continue
		}
		if rows[i].UserObjectID == uuid.Nil {
			continue
		}
		ac := rows[i].AccessClaim
		set[idx].Claim = &ac
	}
	return set, nil
}

func (d *DB) UserAccess(ctx context.Context, actorID, targetUserID uuid.UUID, requiredAccess ClaimAccess) (*AccessUser, error) {
	users := make([]AccessUser, 0)
	err := d.NewSelect().
		TableExpr(`claimius.get_user_access(?, ?, ?) AS u`,
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
			(
				SELECT DISTINCT c.sa_root_id
				FROM claimius.user_claim uc
				JOIN claimius.claim c ON c.id = uc.claim_id
				WHERE uc.user_id = ?
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
