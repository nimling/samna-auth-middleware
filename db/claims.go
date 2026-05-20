package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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
	var (
		name        interface{}
		description interface{}
		access      interface{}
		inherits    interface{}
	)
	if claim.HasKey("name") {
		name = claim.Name
	}
	if claim.HasKey("description") {
		description = claim.Description
	}
	if claim.HasKey("access") {
		access = claim.Access
	}
	if claim.HasKey("inherits") {
		inherits = claim.Inherits
	}
	_, err := d.NewRaw(`
		SELECT claimius.update_claim(?, ?, ?, ?, ?)
	`,
		claimID,
		name,
		description,
		access,
		inherits,
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

func (d *DB) PatchClaim(ctx context.Context, ac *AccessClaim, claimID uuid.UUID, claim *Claim) (*Claim, error) {
	return d.PutClaim(ctx, ac, claimID, claim)
}

func (d *DB) DeleteClaim(ctx context.Context, ac *AccessClaim, claimID uuid.UUID) (*Claim, error) {
	if ac == nil || ac.UserClaimID == nil {
		return nil, ErrNoAccess
	}
	var claim Claim
	err := d.NewSelect().
		Model(&claim).
		ExcludeColumn("app_id").
		ModelTableExpr("claimius.remove_claim(?, ?) AS claim", claimID, *ac.UserClaimID).
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
			p_ref_id        := ?,
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
		co.RefID,
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
	var rowID uuid.UUID
	err := d.NewSelect().
		ColumnExpr("id").
		Table("claimius.claim_object").
		Where("claim_id = ?", claimID).
		Where("object_id = ?", objectID).
		Where("sa_deleted_at IS NULL").
		Scan(ctx, &rowID)
	if err != nil {
		return nil, err
	}
	var co ClaimObject
	err = d.NewSelect().
		Model(&co).
		ExcludeColumn("app_id").
		ModelTableExpr("claimius.remove_claim_object(?, ?) AS claim_object", rowID, *ac.UserClaimID).
		Scan(ctx)
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
	var rowID uuid.UUID
	err := d.NewSelect().
		ColumnExpr("id").
		Table("claimius.user_claim").
		Where("claim_id = ?", claimID).
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		Scan(ctx, &rowID)
	if err != nil {
		return nil, err
	}
	var uc UserClaim
	err = d.NewSelect().
		Model(&uc).
		ExcludeColumn("app_id").
		ModelTableExpr("claimius.remove_user_claim(?, ?) AS user_claim", rowID, *ac.UserClaimID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &uc, nil
}

func (d *DB) CreateClaim(ctx context.Context, ac *AccessClaim, p CreateClaimParams) (*Claim, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	if p.Name == "" {
		return nil, fmt.Errorf("claim name is required")
	}
	claimType := p.Type
	if claimType == "" {
		claimType = CLAIM_TYPE_VIRTUAL
	}
	inherits := true
	if p.Inherits != nil {
		inherits = *p.Inherits
	}
	claim := &Claim{
		Name:        p.Name,
		Description: p.Description,
		Access:      int(p.Access),
		Inherits:    inherits,
		Type:        claimType,
		OwnerID:     ac.OwnerID,
	}
	created, err := d.PostClaim(ctx, ac, claim)
	if err != nil {
		return nil, err
	}
	return created.With(ctx, ac, d), nil
}

func (c *Claim) With(ctx context.Context, ac *AccessClaim, d *DB) *Claim {
	c.sessionCtx = ctx
	c.sessionActor = ac
	c.sessionDB = d
	return c
}

func (c *Claim) requireSession() error {
	if c == nil {
		return fmt.Errorf("claim is nil")
	}
	if c.sessionDB == nil || c.sessionActor == nil || c.sessionCtx == nil {
		return fmt.Errorf("claim session not attached, call With(ctx, ac, d) first")
	}
	return nil
}

func (c *Claim) AssignObject(target any) (*ClaimObject, error) {
	if err := c.requireSession(); err != nil {
		return nil, err
	}
	var co ClaimObject
	switch v := target.(type) {
	case *ClaimObject:
		if v == nil {
			return nil, fmt.Errorf("target ClaimObject is nil")
		}
		co = *v
		if co.ObjectType != nil {
			qualified, err := ResolveObjectType(string(*co.ObjectType))
			if err != nil {
				return nil, err
			}
			co.ObjectType = &qualified
		}
	default:
		otype, err := ObjectTypeOf(target)
		if err != nil {
			return nil, err
		}
		id, err := extractModelID(target)
		if err != nil {
			return nil, err
		}
		co.ObjectID = &id
		co.ObjectType = &otype
		co.Inherits = true
	}
	return c.sessionDB.PostClaimObject(c.sessionCtx, c.sessionActor, c.ID, &co)
}

func (c *Claim) AssignUser(userIDs ...uuid.UUID) ([]UserClaim, error) {
	if err := c.requireSession(); err != nil {
		return nil, err
	}
	results := make([]UserClaim, 0, len(userIDs))
	for _, uid := range userIDs {
		if uid == uuid.Nil {
			continue
		}
		var existing UserClaim
		err := c.sessionDB.NewSelect().
			Model(&existing).
			ExcludeColumn("app_id").
			Where("claim_id = ?", c.ID).
			Where("user_id = ?", uid).
			Where("app_id = claimius.get_disciple_app_id()").
			Where("sa_deleted_at IS NULL").
			Limit(1).
			Scan(c.sessionCtx)
		if err == nil {
			results = append(results, existing)
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return results, err
		}
		uc := UserClaim{UserID: uid}
		created, err := c.sessionDB.PostClaimUser(c.sessionCtx, c.sessionActor, c.ID, &uc)
		if err != nil {
			return results, err
		}
		results = append(results, *created)
	}
	return results, nil
}

func (c *Claim) ReCreateFor(access ClaimAccess, opts ReCreateForOptions, userIDs ...uuid.UUID) (*Claim, error) {
	if err := c.requireSession(); err != nil {
		return nil, err
	}
	name := c.Name
	if opts.Name != nil {
		name = *opts.Name
	}
	inherits := c.Inherits
	params := CreateClaimParams{
		Name:        name,
		Description: c.Description,
		Access:      access,
		Inherits:    &inherits,
		Type:        c.Type,
	}
	newClaim, err := c.sessionDB.CreateClaim(c.sessionCtx, c.sessionActor, params)
	if err != nil {
		return nil, err
	}

	bindings := c.ClaimObjects
	if len(bindings) == 0 {
		err := c.sessionDB.NewSelect().
			Model(&bindings).
			ExcludeColumn("app_id").
			Where("claim_id = ?", c.ID).
			Where("sa_deleted_at IS NULL").
			Scan(c.sessionCtx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	for i := range bindings {
		src := bindings[i]
		clone := ClaimObject{
			ObjectID:   src.ObjectID,
			ObjectType: src.ObjectType,
			Reason:     src.Reason,
			Scope:      src.Scope,
			RefID:      src.RefID,
			Inherits:   src.Inherits,
			Access:     src.Access,
		}
		if _, err := newClaim.AssignObject(&clone); err != nil {
			return nil, err
		}
	}

	if opts.CopyUsers {
		existingUsers := make([]UserClaim, 0)
		err := c.sessionDB.NewSelect().
			Model(&existingUsers).
			ExcludeColumn("app_id").
			Where("claim_id = ?", c.ID).
			Where("sa_deleted_at IS NULL").
			Scan(c.sessionCtx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		copyIDs := make([]uuid.UUID, 0, len(existingUsers))
		for _, u := range existingUsers {
			copyIDs = append(copyIDs, u.UserID)
		}
		if len(copyIDs) > 0 {
			if _, err := newClaim.AssignUser(copyIDs...); err != nil {
				return nil, err
			}
		}
	}

	if len(userIDs) > 0 {
		if _, err := newClaim.AssignUser(userIDs...); err != nil {
			return nil, err
		}
	}
	return newClaim, nil
}
