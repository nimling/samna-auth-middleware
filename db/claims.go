package db

import (
	"context"

	"github.com/google/uuid"
)

func (d *DB) GetAccess(ctx context.Context, userID, objectID uuid.UUID, objectType string, requiredAccess ClaimAccess) (*AccessClaim, error) {
	claims := make([]AccessClaim, 0)
	err := d.NewSelect().
		TableExpr("claimius.get_access(?, claimius.get_disciple_app_id(), ?, ?, ?) AS claim", userID, objectID, objectType, requiredAccess).
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

func (d *DB) GetScopeAccess(ctx context.Context, userID uuid.UUID, scopeKey string, requiredAccess ClaimAccess) (*AccessClaim, error) {
	claims := make([]AccessClaim, 0)
	err := d.NewSelect().
		TableExpr("claimius.get_access(?, claimius.get_disciple_app_id(), ?::text, ?) AS claim", userID, scopeKey, requiredAccess).
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
	err := d.NewSelect().
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

func (d *DB) GetClaim(ctx context.Context, userID, claimID uuid.UUID, requiredAccess ClaimAccess, relations ...RelationFunc) (*Claim, error) {
	var claim Claim
	q := d.NewSelect().
		Model(&claim).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("claim.id = ?", claimID).
		Where("EXISTS (SELECT 1 FROM claimius.get_access(?, claimius.get_disciple_app_id(), claim.id, ?, ?))", userID, OBJECT_TYPE_CLAIM, requiredAccess).
		Limit(1)
	q = ApplyRelations(q, relations)
	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &claim, nil
}

func (d *DB) GetClaims(ctx context.Context, userID uuid.UUID, requiredAccess ClaimAccess, relations ...RelationFunc) ([]Claim, error) {
	claims := make([]Claim, 0)
	q := d.NewSelect().
		Model(&claims).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("claim.id IN (SELECT id FROM claimius.get_claims(?, claimius.get_disciple_app_id(), ?))", userID, requiredAccess)
	q = ApplyRelations(q, relations)
	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}
	return claims, nil
}

func (d *DB) GetClaimAudit(ctx context.Context, claimID uuid.UUID) ([]Audit, error) {
	audits := make([]Audit, 0)
	err := d.NewSelect().
		Model(&audits).
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("object_type = ?", OBJECT_TYPE_CLAIM).
		Where("object_id = ?", claimID).
		OrderExpr("sa_created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return audits, nil
}

func (d *DB) Search(ctx context.Context, userID uuid.UUID, query string, objectTypes []string, requiredAccess ClaimAccess, limit int, offset int) ([]SearchResult, error) {
	results := make([]SearchResult, 0)
	err := d.NewRaw(
		`SELECT * FROM claimius.search(?, claimius.get_disciple_app_id(), ?, ?::text[], ?, ?, ?)`,
		userID, query, objectTypes, requiredAccess, limit, offset,
	).Scan(ctx, &results)
	if err != nil {
		return nil, err
	}
	return results, nil
}

func (d *DB) GetAudit(ctx context.Context, userID uuid.UUID, requiredAccess ClaimAccess) ([]Audit, error) {
	audits := make([]Audit, 0)
	err := d.NewSelect().
		TableExpr("claimius.get_audit(?, claimius.get_disciple_app_id(), ?) AS audit", userID, requiredAccess).
		Column("audit.*").
		Scan(ctx, &audits)
	if err != nil {
		return nil, err
	}
	return audits, nil
}

func (d *DB) HasAccess(ctx context.Context, userID, objectID uuid.UUID, objectType string, requiredAccess ClaimAccess) bool {
	claim, err := d.GetAccess(ctx, userID, objectID, objectType, requiredAccess)
	return err == nil && claim != nil
}

func (d *DB) GetObjectClaims(ctx context.Context, userID, objectID uuid.UUID, objectType string) ([]Claim, error) {
	claims := make([]Claim, 0)
	err := d.NewSelect().
		Model(&claims).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("claim.id IN (SELECT id FROM claimius.get_claims(?, claimius.get_disciple_app_id(), ?, ?))", userID, objectID, objectType).
		Scan(ctx)
	return claims, err
}

func (d *DB) GetOwnerGraph(ctx context.Context, userID uuid.UUID, startID *uuid.UUID, depth int, requiredAccess ClaimAccess) ([]GraphNode, error) {
	nodes := make([]GraphNode, 0)
	err := d.NewSelect().
		TableExpr("claimius.get_owner_graph(p_user_id := ?, p_app_id := claimius.get_disciple_app_id(), p_start_id := ?, p_depth := ?, p_required_access := ?) AS graph", userID, startID, depth, requiredAccess).
		Column("graph.*").
		Scan(ctx, &nodes)
	return nodes, err
}

func (d *DB) GetClaimGraph(ctx context.Context, userID uuid.UUID, startID *uuid.UUID, depth int, requiredAccess ClaimAccess) ([]GraphNode, error) {
	nodes := make([]GraphNode, 0)
	err := d.NewSelect().
		TableExpr("claimius.get_claim_graph(p_user_id := ?, p_app_id := claimius.get_disciple_app_id(), p_start_id := ?, p_depth := ?, p_required_access := ?) AS graph", userID, startID, depth, requiredAccess).
		Column("graph.*").
		Scan(ctx, &nodes)
	return nodes, err
}

func (d *DB) PostClaim(ctx context.Context, in Claim) (*Claim, error) {
	var id uuid.UUID
	err := d.NewRaw(`
		SELECT (claimius.create_claim(
			claimius.get_disciple_app_id(),
			?, ?, ?, ?, ?, ?, ?, ?::claimius.claim_type
		) -> 'claim' ->> 'id')::uuid
	`, in.Name, in.Description, in.Access, in.OwnerID, in.RootID, in.CreatedBy,
		in.Inherits, string(in.Type)).Scan(ctx, &id)
	if err != nil {
		return nil, err
	}
	out := Claim{}
	if err := d.NewSelect().Model(&out).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").ExcludeColumn("app_id").Where("id = ?", id).Scan(ctx); err != nil {
		return nil, err
	}
	return &out, nil
}

func (d *DB) PutClaim(ctx context.Context, claimID uuid.UUID, in Claim) (*Claim, error) {
	_, err := d.NewRaw(`
		SELECT claimius.update_claim(?, ?, ?, ?, ?)
	`, claimID, in.Name, in.Description, in.Access, in.Inherits).Exec(ctx)
	if err != nil {
		return nil, err
	}
	out := Claim{}
	if err := d.NewSelect().Model(&out).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").ExcludeColumn("app_id").Where("id = ?", claimID).Scan(ctx); err != nil {
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
			p_app_id        := claimius.get_disciple_app_id(),
			p_claim_id      := ?,
			p_sa_owner_id   := ?,
			p_sa_root_id    := ?,
			p_sa_created_by := ?,
			p_object_id     := ?,
			p_object_type   := ?,
			p_sa_access     := ?,
			p_inherits      := ?,
			p_reason        := ?,
			p_ref_id        := NULL::uuid,
			p_scope         := ?
		) -> 'claim_object' ->> 'id')::uuid
	`, claimID, in.OwnerID, in.RootID, in.CreatedBy,
		in.ObjectID, in.ObjectType, in.Access, in.Inherits, in.Reason, in.Scope).Scan(ctx, &id)
	if err != nil {
		return nil, err
	}
	out := ClaimObject{}
	if err := d.NewSelect().Model(&out).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").ExcludeColumn("app_id").Where("id = ?", id).Scan(ctx); err != nil {
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
	if err := d.NewSelect().Model(&out).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").ExcludeColumn("app_id").Where("id = ?", id).Scan(ctx); err != nil {
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
