package shared

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/nimling/nimtel"
	"github.com/uptrace/bun"
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
		UserID:       userID,
		Access:       requiredAccess,
		OwnerID:      userID,
		RootID:       userID,
		DirectGrant:  true,
	}
}

const mutationAccessQuery = "SELECT * FROM claimius.mutation_access($1::uuid, $2::text, $3::text, $4::int)"

func (d *DB) MutationAccess(ctx context.Context, userID, objectID uuid.UUID, objectType string, requiredAccess ClaimAccess) (*AccessClaim, error) {
	if userID == AnonymousUserID {
		return d.getGuestAccess(ctx, objectID, objectType, requiredAccess)
	}
	if objectType == OBJECT_TYPE_USER && userID == objectID {
		return selfAccessClaim(userID, requiredAccess), nil
	}
	claims := make([]AccessClaim, 0)
	err := d.prepared(ctx, d.DB, &claims, mutationAccessQuery, userID, objectID, objectType, requiredAccess)
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
	d.Usage.Add(nil, userID, objectType, objectID)
	nimtel.Organization(ctx, claims[0].RootID, claims[0].OwnerID)
	claims[0].UserID = userID
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
	err := d.prepared(ctx, d.DB, &rows, `
		WITH inputs AS (
			SELECT user_id, object_id, object_type, required_access,
			       row_number() OVER () AS idx
			FROM unnest($1::uuid[], $2::uuid[], $3::text[], $4::int[]) AS u(user_id, object_id, object_type, required_access)
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
			g.direct_grant,
			g.level
		FROM inputs
		LEFT JOIN LATERAL claimius.resolve_access(inputs.user_id, inputs.object_id::text, inputs.object_type, inputs.required_access) g ON TRUE
		ORDER BY inputs.idx
	`, pq.Array(userIDs), pq.Array(objectIDs), pq.Array(objectTypes), pq.Array(requiredAccesses))
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
		ac.UserID = requests[idx].UserID
		set[idx].Claim = &ac
		d.Usage.Add(nil, requests[idx].UserID, requests[idx].ObjectType, requests[idx].ObjectID)
	}
	for i, r := range requests {
		if r.UserID != AnonymousUserID {
			continue
		}
		if ac, guestErr := d.getGuestAccess(ctx, r.ObjectID, r.ObjectType, r.RequiredAccess); guestErr == nil {
			set[i].Claim = ac
		}
	}
	return set, nil
}

func (d *DB) ScopeAccess(ctx context.Context, userID uuid.UUID, scopePath string, requiredAccess ClaimAccess) ([]AccessClaim, error) {
	type scopeAccessRow struct {
		UserClaimID   uuid.UUID        `bun:"user_claim_id"`
		ClaimID       uuid.UUID        `bun:"claim_id"`
		ClaimName     string           `bun:"claim_name"`
		Access        ClaimAccess      `bun:"sa_access"`
		OwnerID       uuid.UUID        `bun:"sa_owner_id"`
		RootID        uuid.UUID        `bun:"sa_root_id"`
		ObjectRowID   uuid.UUID        `bun:"object_row_id"`
		ObjectScope   *json.RawMessage `bun:"object_scope,type:jsonb"`
		ObjectAccess  *ClaimAccess     `bun:"object_access"`
		ObjectInherit bool             `bun:"object_inherits"`
		Level         int              `bun:"level"`
	}
	rows := make([]scopeAccessRow, 0)
	err := d.Read(ctx, func(ctx context.Context, db *bun.DB) error {
		return d.prepared(ctx, db, &rows, "SELECT * FROM claimius.get_scope_access($1::uuid, $2::jsonpath, $3::int)", userID, scopePath, requiredAccess)
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []AccessClaim{}, nil
		}
		return nil, ErrInternalServer
	}

	order := make([]uuid.UUID, 0)
	byClaim := make(map[uuid.UUID]*AccessClaim)
	for i := range rows {
		r := rows[i]
		ac, ok := byClaim[r.ClaimID]
		if !ok {
			claimID := r.ClaimID
			userClaimID := r.UserClaimID
			claimName := r.ClaimName
			ac = &AccessClaim{
				UserClaimID: &userClaimID,
				ClaimID:     &claimID,
				ClaimName:   &claimName,
				Access:      r.Access,
				OwnerID:     r.OwnerID,
				RootID:      r.RootID,
				Level:       r.Level,
			}
			byClaim[r.ClaimID] = ac
			order = append(order, r.ClaimID)
		} else {
			ac.Access |= r.Access
		}
		ac.Objects = append(ac.Objects, ClaimObject{
			ID:       r.ObjectRowID,
			ClaimID:  r.ClaimID,
			Scope:    r.ObjectScope,
			Access:   r.ObjectAccess,
			Inherits: r.ObjectInherit,
			OwnerID:  r.OwnerID,
			RootID:   r.RootID,
		})
	}
	result := make([]AccessClaim, 0, len(order))
	for _, claimID := range order {
		result = append(result, *byClaim[claimID])
	}
	return result, nil
}

func (d *DB) UserAccess(ctx context.Context, actorID uuid.UUID, target string, requiredAccess ClaimAccess) (*AccessUser, error) {
	users := make([]AccessUser, 0)
	err := d.prepared(ctx, d.DB, &users, "SELECT * FROM claimius.get_user_access($1::uuid, $2::text, $3::int)", actorID, target, requiredAccess)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoAccess
		}
		return nil, ErrInternalServer
	}
	if len(users) == 0 {
		gateErr := d.prepared(ctx, d.DB, &[]AccessClaim{}, mutationAccessQuery, actorID, target, OBJECT_TYPE_USER, requiredAccess)
		var pgErr pgdriver.Error
		if errors.As(gateErr, &pgErr) && pgErr.Field('C') == "P0404" {
			return nil, ErrNotFound
		}
		return nil, ErrNoAccess
	}
	d.Usage.Add(nil, actorID, OBJECT_TYPE_USER, users[0].TargetUserID)
	return &users[0], nil
}

func (d *DB) RootAccess(ctx context.Context, userID uuid.UUID, requiredAccess ClaimAccess) (*AccessClaim, error) {
	claims := make([]AccessClaim, 0)
	err := d.prepared(ctx, d.DB, &claims, `SELECT claim.*
		FROM (
			SELECT DISTINCT c.sa_root_id
			FROM claimius.user_claim uc
			JOIN claimius.claim c ON c.id = uc.claim_id
			JOIN claimius.organization o ON o.id = c.sa_root_id AND o.sa_deleted_at IS NULL
			WHERE uc.user_id = $1::uuid
			  AND uc.sa_deleted_at IS NULL
			  AND c.sa_deleted_at IS NULL
		) root
		CROSS JOIN LATERAL claimius.get_access($1::uuid, root.sa_root_id, $2::text, $3::int) AS claim
		ORDER BY root.sa_root_id
		LIMIT 1`, userID, OBJECT_TYPE_ORGANIZATION, requiredAccess)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoAccess
		}
		return nil, ErrInternalServer
	}
	if len(claims) == 0 {
		return nil, ErrNoAccess
	}
	nimtel.Organization(ctx, claims[0].RootID, claims[0].OwnerID)
	claims[0].UserID = userID
	return &claims[0], nil
}

func (d *DB) PostAccessDiffDrain(ctx context.Context, limitPairs int) (int, error) {
	var settled int
	err := d.NewRaw("SELECT claimius.drain_access_diff(?)", limitPairs).Scan(ctx, &settled)
	return settled, err
}
