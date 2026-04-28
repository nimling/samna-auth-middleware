package db

import (
	"context"

	"github.com/google/uuid"
)

func (d *DB) GetAccess(ctx context.Context, userID, objectID uuid.UUID, objectType string, accessLevel ClaimAccessLevel) (*AccessClaim, error) {
	var claims []AccessClaim
	err := d.NewSelect().
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

func (d *DB) GetClaim(ctx context.Context, userID, claimID uuid.UUID, accessLevel ClaimAccessLevel, relations ...RelationFunc) (*Claim, error) {
	var claim Claim
	q := d.NewSelect().
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
	var claims []Claim
	q := d.NewSelect().
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
	var audits []Audit
	err := d.NewSelect().
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
	var results []SearchResult
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
	var audits []Audit
	err := d.NewSelect().
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
	var claims []Claim
	err := d.NewSelect().
		Model(&claims).
		Where("claim.id IN (SELECT id FROM claimius.get_claims(?, claimius.get_disciple_app_id(), ?, ?))", userID, objectID, objectType).
		Scan(ctx)
	return claims, err
}

func (d *DB) GetOwnerGraph(ctx context.Context, userID uuid.UUID, accessLevel ClaimAccessLevel) ([]GraphNode, error) {
	var nodes []GraphNode
	err := d.NewSelect().
		TableExpr("claimius.get_owner_graph(?, claimius.get_disciple_app_id(), ?) AS graph", userID, accessLevel).
		Column("graph.*").
		Scan(ctx, &nodes)
	return nodes, err
}

func (d *DB) GetClaimGraph(ctx context.Context, userID uuid.UUID, accessLevel ClaimAccessLevel) ([]GraphNode, error) {
	var nodes []GraphNode
	err := d.NewSelect().
		TableExpr("claimius.get_claim_graph(?, claimius.get_disciple_app_id(), ?) AS graph", userID, accessLevel).
		Column("graph.*").
		Scan(ctx, &nodes)
	return nodes, err
}
