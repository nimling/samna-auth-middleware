package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

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
	var rootID uuid.UUID
	if err := d.NewSelect().
		TableExpr("claimius.organization").
		Column("sa_root_id").
		Where("id = ?", ac.OwnerID).
		Where("sa_deleted_at IS NULL").
		Scan(ctx, &rootID); err != nil {
		return nil, err
	}
	claim := &Claim{
		Name:        p.Name,
		Description: p.Description,
		Access:      p.Access,
		Inherits:    inherits,
		Type:        claimType,
		OwnerID:     ac.OwnerID,
		RootID:      rootID,
		CreatedBy:   ac.UserClaimID,
	}
	if err := d.NewInsert().Model(claim).With(ac).Scan(ctx, claim); err != nil {
		return nil, err
	}
	return claim.With(ctx, ac, d), nil
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
		id, err := ModelID(target)
		if err != nil {
			return nil, err
		}
		co.ObjectID = &id
		co.ObjectType = &otype
		co.Inherits = true
	}
	co.ClaimID = c.ID
	if err := c.sessionDB.NewInsert().Model(&co).With(c.sessionActor).Scan(c.sessionCtx, &co); err != nil {
		return nil, err
	}
	return &co, nil
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
		uc := UserClaim{ClaimID: c.ID, UserID: uid}
		if err := c.sessionDB.NewInsert().Model(&uc).With(c.sessionActor).Scan(c.sessionCtx, &uc); err != nil {
			return results, err
		}
		results = append(results, uc)
	}
	return results, nil
}

func expandAccess(a ClaimAccess) ClaimAccess {
	if a&ACCESS_OWNER == ACCESS_OWNER {
		a |= ACCESS_WRITE | ACCESS_READ | ACCESS_EXECUTE
	}
	if a&ACCESS_WRITE == ACCESS_WRITE {
		a |= ACCESS_READ | ACCESS_EXECUTE
	}
	if a&ACCESS_EXECUTE == ACCESS_EXECUTE {
		a |= ACCESS_READ
	}
	return a
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

	expanded := expandAccess(access)
	if len(bindings) > 0 {
		primary := bindings[0]
		existing := &Claim{}
		err := c.sessionDB.NewSelect().
			Model(existing).
			ExcludeColumn("app_id").
			Where("claim.sa_deleted_at IS NULL").
			Where("claim.type = ?", c.Type).
			Where("claim.sa_access = ?", expanded).
			Where("claim.inherits = ?", inherits).
			Where("claim.name = ?", name).
			Where("claim.sa_owner_id = ?", c.OwnerID).
			Where(`EXISTS (
				SELECT 1 FROM claimius.claim_object co
				WHERE co.claim_id = claim.id
				  AND co.sa_deleted_at IS NULL
				  AND co.object_id = ?
				  AND co.object_type = ?
			)`, primary.ObjectID, primary.ObjectType).
			Limit(1).
			Scan(c.sessionCtx)
		if err == nil {
			existing.With(c.sessionCtx, c.sessionActor, c.sessionDB)
			if len(userIDs) > 0 {
				if _, err := existing.AssignUser(userIDs...); err != nil {
					return nil, err
				}
			}
			return existing, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

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
