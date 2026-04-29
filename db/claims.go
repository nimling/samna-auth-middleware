package db

import (
	"context"

	"github.com/google/uuid"
)

func (d *DB) GetAccess(ctx context.Context, userID, objectID uuid.UUID, objectType string, accessLevel ClaimAccessLevel) (*AccessClaim, error) {
	claims := make([]AccessClaim, 0)
	err := d.NewSelect().ExcludeColumn("app_id").
		TableExpr("claimius.get_access(?, claimius.get_disciple_app_id(), ?, ?, ?) AS claim", userID, objectID, objectType, accessLevel).
		Column("claim.*").
		Scan(ctx, &claims)
	if err != nil {
		return nil, err
	}
	if len(claims) == 0 {
		return nil, nil
	}
	return &claims[0], nil
}

func (d *DB) GetDirectAccess(ctx context.Context, userID, objectID uuid.UUID, objectType string) (*AccessClaim, error) {
	claims := make([]AccessClaim, 0)
	err := d.NewSelect().ExcludeColumn("app_id").
		TableExpr("claimius.get_direct_access(?, claimius.get_disciple_app_id(), ?, ?) AS claim", userID, objectID, objectType).
		Column("claim.*").
		Scan(ctx, &claims)
	if err != nil {
		return nil, err
	}
	if len(claims) == 0 {
		return nil, nil
	}
	return &claims[0], nil
}

func (d *DB) GetClaim(ctx context.Context, userID, claimID uuid.UUID, accessLevel ClaimAccessLevel, relations ...RelationFunc) (*Claim, error) {
	var claim Claim
	q := d.NewSelect().ExcludeColumn("app_id").
		Model(&claim).
		Where("claim.id = ?", claimID).
		Where("claim.id IN (SELECT id FROM claimius.get_claims(?, claimius.get_disciple_app_id(), ?))", userID, accessLevel).
		Limit(1)
	q = ApplyRelations(q, relations)
	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &claim, nil
}

func (d *DB) GetClaims(ctx context.Context, userID uuid.UUID, accessLevel ClaimAccessLevel, relations ...RelationFunc) ([]Claim, error) {
	claims := make([]Claim, 0)
	q := d.NewSelect().ExcludeColumn("app_id").
		Model(&claims).
		Where("claim.id IN (SELECT id FROM claimius.get_claims(?, claimius.get_disciple_app_id(), ?))", userID, accessLevel)
	q = ApplyRelations(q, relations)
	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}
	return claims, nil
}

func (d *DB) GetClaimAudit(ctx context.Context, claimID uuid.UUID) ([]Audit, error) {
	audits := make([]Audit, 0)
	err := d.NewSelect().ExcludeColumn("app_id").
		Model(&audits).
		Where("object_type = ?", OBJECT_TYPE_CLAIM).
		Where("object_id = ?", claimID).
		OrderExpr("sa_created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return audits, nil
}

type SearchResult struct {
	ObjectID    uuid.UUID  `bun:"object_id" json:"object_id"`
	ObjectType  string     `bun:"object_type" json:"object_type"`
	UserClaimID *uuid.UUID `bun:"user_claim_id" json:"user_claim_id"`
	Name        *string    `bun:"sa_name" json:"name"`
	Description *string    `bun:"sa_description" json:"description"`
	Link        *string    `bun:"sa_link" json:"link"`
	AccessLevel int        `bun:"sa_access_level" json:"access_level"`
	OwnerID     *uuid.UUID `bun:"sa_owner_id" json:"owner_id"`
	LocationID  *uuid.UUID `bun:"sa_location_id" json:"location_id"`
	DirectGrant bool       `bun:"direct_grant" json:"direct_grant"`
	Rank        float32    `bun:"rank" json:"rank"`
}

func (d *DB) Search(ctx context.Context, userID uuid.UUID, query string, objectTypes []string, accessLevel ClaimAccessLevel, limit int, offset int) ([]SearchResult, error) {
	results := make([]SearchResult, 0)
	err := d.NewRaw(
		`SELECT * FROM claimius.search_objects(?, claimius.get_disciple_app_id(), ?, ?::text[], ?, ?, ?)`,
		userID, query, objectTypes, accessLevel, limit, offset,
	).Scan(ctx, &results)
	if err != nil {
		return nil, err
	}
	return results, nil
}

func (d *DB) GetAudit(ctx context.Context, userID uuid.UUID, accessLevel ClaimAccessLevel) ([]Audit, error) {
	audits := make([]Audit, 0)
	err := d.NewSelect().ExcludeColumn("app_id").
		TableExpr("claimius.get_audit(?, claimius.get_disciple_app_id(), ?) AS audit", userID, accessLevel).
		Column("audit.*").
		Scan(ctx, &audits)
	if err != nil {
		return nil, err
	}
	return audits, nil
}

func (d *DB) HasAccess(ctx context.Context, userID, objectID uuid.UUID, objectType string, accessLevel ClaimAccessLevel) bool {
	claim, err := d.GetAccess(ctx, userID, objectID, objectType, accessLevel)
	return err == nil && claim != nil
}

func (d *DB) GetObjectClaims(ctx context.Context, userID, objectID uuid.UUID, objectType string) ([]Claim, error) {
	claims := make([]Claim, 0)
	err := d.NewSelect().ExcludeColumn("app_id").
		Model(&claims).
		Where("claim.id IN (SELECT id FROM claimius.get_claims(?, claimius.get_disciple_app_id(), ?, ?))", userID, objectID, objectType).
		Scan(ctx)
	return claims, err
}

func (d *DB) GetOwnerGraph(ctx context.Context, userID uuid.UUID, accessLevel ClaimAccessLevel) ([]GraphNode, error) {
	nodes := make([]GraphNode, 0)
	err := d.NewSelect().ExcludeColumn("app_id").
		TableExpr("claimius.get_owner_graph(p_user_id := ?, p_app_id := claimius.get_disciple_app_id(), p_min_level := ?) AS graph", userID, accessLevel).
		Column("graph.*").
		Scan(ctx, &nodes)
	return nodes, err
}

func (d *DB) GetClaimGraph(ctx context.Context, userID uuid.UUID, accessLevel ClaimAccessLevel) ([]GraphNode, error) {
	nodes := make([]GraphNode, 0)
	err := d.NewSelect().ExcludeColumn("app_id").
		TableExpr("claimius.get_claim_graph(p_user_id := ?, p_app_id := claimius.get_disciple_app_id(), p_min_level := ?) AS graph", userID, accessLevel).
		Column("graph.*").
		Scan(ctx, &nodes)
	return nodes, err
}

func (d *DB) PostClaim(ctx context.Context, in Claim) (*Claim, error) {
	var id uuid.UUID
	err := d.NewRaw(`
		SELECT (claimius.create_claim(
			claimius.get_disciple_app_id(),
			?, ?, ?, ?, ?, ?, ?, ?, ?::claimius.claim_type
		) -> 'claim' ->> 'id')::uuid
	`, in.Name, in.Description, in.AccessLevel, in.OwnerID, in.RootID, in.CreatedBy,
		in.Inherits, in.IsDeny, string(in.Type)).Scan(ctx, &id)
	if err != nil {
		return nil, err
	}
	out := Claim{}
	if err := d.NewSelect().ExcludeColumn("app_id").Model(&out).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, err
	}
	return &out, nil
}

func (d *DB) PutClaim(ctx context.Context, claimID uuid.UUID, in Claim) (*Claim, error) {
	_, err := d.NewRaw(`
		SELECT claimius.update_claim(?, ?, ?, ?, ?, ?)
	`, claimID, in.Name, in.Description, in.AccessLevel, in.Inherits, in.IsDeny).Exec(ctx)
	if err != nil {
		return nil, err
	}
	out := Claim{}
	if err := d.NewSelect().ExcludeColumn("app_id").Model(&out).Where("id = ?", claimID).Scan(ctx); err != nil {
		return nil, err
	}
	return &out, nil
}

func (d *DB) DeleteClaim(ctx context.Context, claimID, deletedBy uuid.UUID) error {
	_, err := d.NewRaw(`SELECT claimius.remove_claim(?, ?)`, claimID, deletedBy).Exec(ctx)
	return err
}

func (d *DB) PostClaimObject(ctx context.Context, claimID uuid.UUID, in ClaimObject) (*ClaimObject, error) {
	var id uuid.UUID
	err := d.NewRaw(`
		SELECT (claimius.assign_claim_object(
			claimius.get_disciple_app_id(),
			?, ?, ?, ?, ?, ?, ?, ?, ?, NULL::uuid, ?
		) -> 'claim_object' ->> 'id')::uuid
	`, claimID, in.ObjectID, in.ObjectType, in.OwnerID, in.RootID, in.CreatedBy,
		in.Level, in.Inherits, in.Reason, in.Function).Scan(ctx, &id)
	if err != nil {
		return nil, err
	}
	out := ClaimObject{}
	if err := d.NewSelect().ExcludeColumn("app_id").Model(&out).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, err
	}
	return &out, nil
}

func (d *DB) DeleteClaimObject(ctx context.Context, claimID, objectID, deletedBy uuid.UUID) error {
	_, err := d.NewRaw(`
		SELECT claimius.remove_claim_object(co.id, ?)
		FROM claimius.claim_object co
		WHERE co.claim_id = ? AND co.object_id = ? AND co.sa_deleted_at IS NULL
	`, deletedBy, claimID, objectID).Exec(ctx)
	return err
}

func (d *DB) PostClaimUser(ctx context.Context, claimID uuid.UUID, in UserClaim) (*UserClaim, error) {
	var id uuid.UUID
	err := d.NewRaw(`
		SELECT (claimius.assign_claim_user(
			claimius.get_disciple_app_id(),
			?, ?, ?, ?, ?, ?, ?
		) -> 'user_claim' ->> 'id')::uuid
	`, claimID, in.UserID, in.OwnerID, in.CreatedBy, in.Reason, in.StartsAt, in.EndsAt).Scan(ctx, &id)
	if err != nil {
		return nil, err
	}
	out := UserClaim{}
	if err := d.NewSelect().ExcludeColumn("app_id").Model(&out).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, err
	}
	return &out, nil
}

func (d *DB) DeleteClaimUser(ctx context.Context, claimID, userID, deletedBy uuid.UUID) error {
	_, err := d.NewRaw(`
		SELECT claimius.remove_user_claim(uc.id, ?)
		FROM claimius.user_claim uc
		WHERE uc.claim_id = ? AND uc.user_id = ? AND uc.sa_deleted_at IS NULL
	`, deletedBy, claimID, userID).Exec(ctx)
	return err
}
