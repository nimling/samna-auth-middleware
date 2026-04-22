package db

import (
	"context"

	"github.com/google/uuid"
)

func (d *DB) GetAccess(ctx context.Context, userID uuid.UUID, objectID *uuid.UUID, accessLevel ClaimAccessLevel) (*AccessClaim, error) {
	var claims []AccessClaim
	err := d.NewSelect().
		TableExpr("samna_auth.get_access(?, ?, ?) AS claim", userID, objectID, accessLevel).
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

func (d *DB) GetClaim(ctx context.Context, userID uuid.UUID, claimID uuid.UUID, accessLevel ClaimAccessLevel, relations ...RelationFunc) (*Claim, error) {
	var claim Claim
	q := d.NewSelect().
		Model(&claim).
		Where("claim.id = ?", claimID).
		Where("claim.id IN (SELECT id FROM samna_auth.get_claims(?, ?))", userID, accessLevel).
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
		Where("claim.id IN (SELECT id FROM samna_auth.get_claims(?, ?))", userID, accessLevel)
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
		Where("object_type = ?", "claim").
		Where("object_id = ?", claimID).
		OrderExpr("sa_created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return audits, nil
}

type SearchResult struct {
	ID          uuid.UUID `bun:"id" json:"id"`
	ObjectType  string    `bun:"object_type" json:"object_type"`
	Name        string    `bun:"name" json:"name"`
	Description string    `bun:"description" json:"description"`
	Rank        float32   `bun:"rank" json:"rank"`
}

func (d *DB) Search(ctx context.Context, userID uuid.UUID, query string, objectTypes []string, accessLevel ClaimAccessLevel, limit int, offset int) ([]SearchResult, error) {
	var results []SearchResult
	err := d.NewRaw(
		`SELECT * FROM samna_auth.search(?, ?, ?::text[], ?, ?, ?)`,
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
		TableExpr("samna_auth.get_audit(?, ?) AS audit", userID, accessLevel).
		Column("audit.*").
		Scan(ctx, &audits)
	if err != nil {
		return nil, err
	}
	return audits, nil
}

func (d *DB) HasAccess(ctx context.Context, userID uuid.UUID, objectID *uuid.UUID, accessLevel ClaimAccessLevel) bool {
	claim, err := d.GetAccess(ctx, userID, objectID, accessLevel)
	return err == nil && claim != nil
}

func (d *DB) GetObjectClaims(ctx context.Context, userID uuid.UUID, objectID uuid.UUID) ([]Claim, error) {
	var claims []Claim
	err := d.NewSelect().
		Model(&claims).
		Where("claim.id IN (SELECT id FROM samna_auth.get_claims(?, ?))", userID, objectID).
		Scan(ctx)
	return claims, err
}

// GetOwnerGraph returns the ownership graph for a user. The graph contains
// organizations and locations visible at the given access level.
func (d *DB) GetOwnerGraph(ctx context.Context, userID uuid.UUID, accessLevel ClaimAccessLevel) ([]GraphNode, error) {
	var nodes []GraphNode
	err := d.NewSelect().
		TableExpr("samna_auth.get_owner_graph(?, ?) AS graph", userID, accessLevel).
		Column("graph.*").
		Scan(ctx, &nodes)
	return nodes, err
}

// GetClaimGraph returns the claim graph for a user. The graph contains claims,
// user claims, users, organizations, locations, and unresolved typed object
// references for any domain objects not managed by the auth schema.
func (d *DB) GetClaimGraph(ctx context.Context, userID uuid.UUID, accessLevel ClaimAccessLevel) ([]GraphNode, error) {
	var nodes []GraphNode
	err := d.NewSelect().
		TableExpr("samna_auth.get_claim_graph(?, ?) AS graph", userID, accessLevel).
		Column("graph.*").
		Scan(ctx, &nodes)
	return nodes, err
}

