package db

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

func (d *DB) GetAccess(ctx context.Context, userID uuid.UUID, objectID *uuid.UUID, accessLevel ClaimAccessLevel) (*ActiveClaim, error) {
	var claims []ActiveClaim
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

func (d *DB) GetClaimSearch(ctx context.Context, userID uuid.UUID, claimID uuid.UUID, accessLevel ClaimAccessLevel) (*Claim, error) {
	var claim Claim
	q := d.NewSelect().
		Model(&claim).
		Where("claim.id = ?", claimID).
		Where("claim.id IN (SELECT id FROM samna_auth.get_claims(?, ?))", userID, accessLevel).
		Where("claim.sa_deleted_at IS NULL").
		Relation("ClaimObjects", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("claim_object.sa_deleted_at IS NULL")
		}).
		Relation("ClaimUsers", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("user_claim.sa_deleted_at IS NULL")
		}).
		Relation("Owner").
		Limit(1)
	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &claim, nil
}

func (d *DB) HasAccess(ctx context.Context, userID uuid.UUID, objectID *uuid.UUID, accessLevel ClaimAccessLevel) bool {
	ac, err := d.GetAccess(ctx, userID, objectID, accessLevel)
	return err == nil && ac != nil
}

func (d *DB) GetObjectClaims(ctx context.Context, userID uuid.UUID, objectID uuid.UUID) ([]Claim, error) {
	var claims []Claim
	err := d.NewSelect().
		Model(&claims).
		Where("claim.id IN (SELECT id FROM samna_auth.get_claims(?, ?))", userID, objectID).
		Scan(ctx)
	return claims, err
}

func (d *DB) InitAuthTables(ctx context.Context, tableNames ...string) error {
	placeholders := make([]string, len(tableNames))
	args := make([]interface{}, len(tableNames))
	for i, name := range tableNames {
		placeholders[i] = "?"
		args[i] = name
	}
	query := "SELECT samna_auth.init_auth_tables(" + strings.Join(placeholders, ", ") + ")"
	_, err := d.NewRaw(query, args...).Exec(ctx)
	return err
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

func (d *DB) GetSyncCursors(ctx context.Context) (map[string]SyncCursor, error) {
	var clients []SamnaClient
	err := d.NewSelect().
		Model(&clients).
		Where("removed_at IS NULL").
		Scan(ctx)
	if err != nil {
		return nil, err
	}

	cursors := make(map[string]SyncCursor)
	for _, c := range clients {
		cursors[c.ObjectType] = SyncCursor{
			UpdatedAt: c.UpdatedAt,
			UpdatedID: c.UpdatedID,
			DeletedAt: c.DeletedAt,
			DeletedID: c.DeletedID,
		}
	}
	return cursors, nil
}

func (d *DB) UpdateSyncCursor(ctx context.Context, objectType string, cursor SyncCursor) error {
	_, err := d.NewInsert().
		Model(&SamnaClient{
			ObjectType: objectType,
			UpdatedAt:  cursor.UpdatedAt,
			UpdatedID:  cursor.UpdatedID,
			DeletedAt:  cursor.DeletedAt,
			DeletedID:  cursor.DeletedID,
		}).
		On("CONFLICT (object_type) DO UPDATE").
		Set("updated_at = EXCLUDED.updated_at").
		Set("updated_id = EXCLUDED.updated_id").
		Set("deleted_at = EXCLUDED.deleted_at").
		Set("deleted_id = EXCLUDED.deleted_id").
		Set("modified_at = CURRENT_TIMESTAMP").
		Exec(ctx)
	return err
}
