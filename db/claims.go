package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

func (d *DB) GetAccess(ctx context.Context, userID, objectID uuid.UUID, objectType string, requiredAccess ClaimAccess) (*AccessClaim, error) {
	claims := make([]AccessClaim, 0)
	err := d.NewSelect().
		TableExpr("claimius.get_access(?, claimius.get_disciple_app_id(), ?, ?, ?) AS claim", userID, objectID, objectType, requiredAccess).
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

func (d *DB) GetScopeAccess(ctx context.Context, userID uuid.UUID, scopeKey string, requiredAccess ClaimAccess) (*AccessClaim, error) {
	claims := make([]AccessClaim, 0)
	err := d.NewSelect().
		TableExpr("claimius.get_access(?, claimius.get_disciple_app_id(), ?::text, ?) AS claim", userID, scopeKey, requiredAccess).
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

func (d *DB) GetDirectAccess(ctx context.Context, userID, objectID uuid.UUID, objectType string) (*AccessClaim, error) {
	claims := make([]AccessClaim, 0)
	err := d.NewSelect().
		TableExpr("claimius.get_direct_access(?, claimius.get_disciple_app_id(), ?, ?) AS claim", userID, objectID, objectType).
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

func (d *DB) GetClaim(ctx context.Context, userID, claimID uuid.UUID, requiredAccess ClaimAccess) (*Claim, error) {
	var claim Claim
	err := d.GetClaimQuery(&claim, userID, claimID, requiredAccess).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &claim, nil
}

func (d *DB) GetClaimQuery(claim *Claim, userID, claimID uuid.UUID, requiredAccess ClaimAccess) *bun.SelectQuery {
	return d.NewSelect().
		Model(claim).
		Where("claim.sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("claim.id = ?", claimID).
		Where("EXISTS (SELECT 1 FROM claimius.get_access(?, claimius.get_disciple_app_id(), claim.id, ?, ?))", userID, OBJECT_TYPE_CLAIM, requiredAccess).
		Limit(1)
}

func (d *DB) GetClaims(ctx context.Context, userID uuid.UUID, requiredAccess ClaimAccess) ([]Claim, error) {
	claims := make([]Claim, 0)
	err := d.GetClaimsQuery(&claims, userID, requiredAccess).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return claims, nil
}

func (d *DB) GetClaimsQuery(claims *[]Claim, userID uuid.UUID, requiredAccess ClaimAccess) *bun.SelectQuery {
	return d.NewSelect().
		Model(claims).
		Where("claim.sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("claim.id IN (SELECT id FROM claimius.get_claims(?, claimius.get_disciple_app_id(), ?))", userID, requiredAccess)
}

func (d *DB) GetClaimAudit(ctx context.Context, actorID, claimID uuid.UUID, requiredAccess ClaimAccess) ([]Audit, error) {
	audits := make([]Audit, 0)
	err := d.NewSelect().
		TableExpr("claimius.get_audit(?, claimius.get_disciple_app_id(), ?) AS audit", actorID, requiredAccess).
		Column("audit.*").
		Where("audit.object_type = ?", OBJECT_TYPE_CLAIM).
		Where("audit.object_id = ?", claimID).
		OrderExpr("audit.sa_created_at DESC").
		Scan(ctx, &audits)
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

func (d *DB) PostClaim(ctx context.Context, ac *AccessClaim, claim *Claim) (*Claim, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	claim.CreatedBy = ac.UserClaimID
	claim.RootID = ac.RootID
	var id uuid.UUID
	err := d.NewRaw(`
		SELECT (claimius.create_claim(
			claimius.get_disciple_app_id(),
			?, ?, ?, ?, ?, ?, ?, ?::claimius.claim_type
		) -> 'claim' ->> 'id')::uuid
	`,
		claim.Name,
		claim.Description,
		claim.Access,
		claim.OwnerID,
		claim.RootID,
		claim.CreatedBy,
		claim.Inherits,
		string(claim.Type),
	).Scan(ctx, &id)
	if err != nil {
		return nil, err
	}
	err = d.NewSelect().
		Model(claim).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return claim, nil
}

func (d *DB) PutClaim(ctx context.Context, ac *AccessClaim, claimID uuid.UUID, claim *Claim) (*Claim, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	_, err := d.NewRaw(`
		SELECT claimius.update_claim(?, ?, ?, ?, ?)
	`,
		claimID,
		claim.Name,
		claim.Description,
		claim.Access,
		claim.Inherits,
	).Exec(ctx)
	if err != nil {
		return nil, err
	}
	err = d.NewSelect().
		Model(claim).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("id = ?", claimID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return claim, nil
}

func (d *DB) DeleteClaim(ctx context.Context, ac *AccessClaim, claimID uuid.UUID) (*Claim, error) {
	if ac == nil || ac.UserClaimID == nil {
		return nil, ErrNoAccess
	}
	_, err := d.NewRaw(`SELECT claimius.remove_claim(?, ?)`, claimID, *ac.UserClaimID).Exec(ctx)
	if err != nil {
		return nil, err
	}
	var claim Claim
	err = d.NewSelect().
		Model(&claim).
		ExcludeColumn("app_id").
		Where("id = ?", claimID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &claim, nil
}

func (d *DB) PostClaimObject(ctx context.Context, ac *AccessClaim, claimID uuid.UUID, co *ClaimObject) (*ClaimObject, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	co.OwnerID = ac.OwnerID
	co.RootID = ac.RootID
	co.CreatedBy = ac.UserClaimID
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
	`,
		claimID,
		co.OwnerID,
		co.RootID,
		co.CreatedBy,
		co.ObjectID,
		co.ObjectType,
		co.Access,
		co.Inherits,
		co.Reason,
		co.Scope,
	).Scan(ctx, &id)
	if err != nil {
		return nil, err
	}
	err = d.NewSelect().
		Model(co).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return co, nil
}

func (d *DB) DeleteClaimObject(ctx context.Context, ac *AccessClaim, claimID, objectID uuid.UUID) (*ClaimObject, error) {
	if ac == nil || ac.UserClaimID == nil {
		return nil, ErrNoAccess
	}
	var co ClaimObject
	err := d.NewSelect().
		Model(&co).
		ExcludeColumn("app_id").
		Where("claim_id = ?", claimID).
		Where("object_id = ?", objectID).
		Where("sa_deleted_at IS NULL").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	_, err = d.NewRaw(`
		SELECT claimius.remove_claim_object(?, ?)
	`, co.ID, *ac.UserClaimID).Exec(ctx)
	if err != nil {
		return nil, err
	}
	return &co, nil
}

func (d *DB) PostClaimUser(ctx context.Context, ac *AccessClaim, claimID uuid.UUID, uc *UserClaim) (*UserClaim, error) {
	if ac == nil || ac.UserClaimID == nil {
		return nil, ErrNoAccess
	}
	uc.OwnerID = ac.OwnerID
	uc.CreatedBy = *ac.UserClaimID
	var id uuid.UUID
	err := d.NewRaw(`
		SELECT (claimius.assign_claim_user(
			claimius.get_disciple_app_id(),
			?, ?, ?, ?, ?, ?, ?
		) -> 'user_claim' ->> 'id')::uuid
	`,
		claimID,
		uc.UserID,
		uc.OwnerID,
		uc.CreatedBy,
		uc.Reason,
		uc.StartsAt,
		uc.EndsAt,
	).Scan(ctx, &id)
	if err != nil {
		return nil, err
	}
	err = d.NewSelect().
		Model(uc).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("id = ?", id).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return uc, nil
}

func (d *DB) DeleteClaimUser(ctx context.Context, ac *AccessClaim, claimID, userID uuid.UUID) (*UserClaim, error) {
	if ac == nil || ac.UserClaimID == nil {
		return nil, ErrNoAccess
	}
	var uc UserClaim
	err := d.NewSelect().
		Model(&uc).
		ExcludeColumn("app_id").
		Where("claim_id = ?", claimID).
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	_, err = d.NewRaw(`
		SELECT claimius.remove_user_claim(?, ?)
	`, uc.ID, *ac.UserClaimID).Exec(ctx)
	if err != nil {
		return nil, err
	}
	return &uc, nil
}
