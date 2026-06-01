package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

func (d *DB) GetUser(ctx context.Context, actorID, userID uuid.UUID) (*SamnaUser, error) {
	var user SamnaUser
	err := d.GetUserQuery(&user, actorID, userID).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (d *DB) GetUserQuery(user *SamnaUser, actorID, userID uuid.UUID) *bun.SelectQuery {
	return d.NewSelect().
		Model(user).
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("samna_user.user_id = ?", userID).
		Where("samna_user.sa_deleted_at IS NULL").
		Where(UserWhere(actorID, ACCESS_READ)).
		Limit(1)
}

func (d *DB) GetUsers(ctx context.Context, userID uuid.UUID, requiredAccess ClaimAccess) ([]SamnaUser, error) {
	users := make([]SamnaUser, 0)
	err := d.GetUsersQuery(&users, userID, requiredAccess).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return users, nil
}

func (d *DB) GetUsersQuery(users *[]SamnaUser, userID uuid.UUID, requiredAccess ClaimAccess) *bun.SelectQuery {
	return d.NewSelect().
		Model(users).
		Where("samna_user.sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where(UserWhere(userID, requiredAccess))
}

func (d *DB) GetUserClaims(ctx context.Context, userID uuid.UUID) ([]UserClaim, error) {
	claims := make([]UserClaim, 0)
	err := d.NewSelect().
		Model(&claims).
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Relation("Owner").
		Where("user_claim.user_id = ?", userID).
		Where("user_claim.sa_deleted_at IS NULL").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return claims, nil
}

type UserRelationFilters struct {
	OrgIDs      []uuid.UUID
	LocIDs      []uuid.UUID
	UsrIDs      []uuid.UUID
	ObjectTypes []ObjectType
	ObjectIDs   []uuid.UUID
	Pinned      *int
	Priority    *int
	ParentID    *uuid.UUID
	SiblingID   *uuid.UUID
	Description *string
}

func (d *DB) GetUserRelations(ctx context.Context, userID uuid.UUID) ([]UserRelation, error) {
	relations := make([]UserRelation, 0)
	err := d.GetUserRelationsQuery(&relations, userID, UserRelationFilters{}).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return relations, nil
}

func (d *DB) GetUserRelationsQuery(relations *[]UserRelation, userID uuid.UUID, f UserRelationFilters) *bun.SelectQuery {
	q := d.NewSelect().
		Model(relations).
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("user_relation.user_id = ?", userID).
		Where("user_relation.sa_deleted_at IS NULL")
	if len(f.OrgIDs) > 0 {
		q = q.Where("user_relation.object_type = ? AND user_relation.object_id IN (?)", OBJECT_TYPE_ORGANIZATION, bun.In(f.OrgIDs))
	}
	if len(f.LocIDs) > 0 {
		q = q.Where("user_relation.object_type = ? AND user_relation.object_id IN (?)", OBJECT_TYPE_LOCATION, bun.In(f.LocIDs))
	}
	if len(f.UsrIDs) > 0 {
		q = q.Where("user_relation.object_type = ? AND user_relation.object_id IN (?)", OBJECT_TYPE_USER, bun.In(f.UsrIDs))
	}
	if len(f.ObjectTypes) > 0 {
		q = q.Where("user_relation.object_type IN (?)", bun.In(f.ObjectTypes))
	}
	if len(f.ObjectIDs) > 0 {
		q = q.Where("user_relation.object_id IN (?)", bun.In(f.ObjectIDs))
	}
	if f.Pinned != nil {
		q = q.Where("user_relation.pinned = ?", *f.Pinned)
	}
	if f.Priority != nil {
		q = q.Where("user_relation.priority = ?", *f.Priority)
	}
	if f.ParentID != nil {
		q = q.Where("user_relation.parent_id = ?", *f.ParentID)
	}
	if f.SiblingID != nil {
		q = q.Where("user_relation.sibling_id = ?", *f.SiblingID)
	}
	if f.Description != nil && *f.Description != "" {
		q = q.Where("user_relation.description ILIKE ?", "%"+*f.Description+"%")
	}
	return q
}

func (d *DB) GetRelation(ctx context.Context, userID uuid.UUID, relationID uuid.UUID) (*UserRelation, error) {
	var relation UserRelation
	err := d.NewSelect().
		Model(&relation).
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("user_relation.id = ?", relationID).
		Where("user_relation.user_id = ?", userID).
		Where("user_relation.sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &relation, nil
}

func (d *DB) insertRelation(ctx context.Context, ac *AccessClaim, relation *UserRelation) (*UserRelation, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	_, err := d.DB.NewInsert().
		Model(relation).
		ExcludeColumn("id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		Value("app_id", "claimius.get_disciple_app_id()").
		Returning("*").
		Exec(ctx, relation)
	if err != nil {
		return nil, err
	}
	return relation, nil
}

func (d *DB) updateRelation(ctx context.Context, ac *AccessClaim, relationID uuid.UUID, relation *UserRelation) (*UserRelation, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	q := d.DB.NewUpdate().
		Model(relation).
		Where("id = ?", relationID).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("id", "app_id", "user_id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		OmitZero().
		Returning("*")
	for _, column := range relation.PatchNulls() {
		q = q.Set("? = NULL", bun.Ident(column))
	}
	_, err := q.Exec(ctx, relation)
	if err != nil {
		return nil, err
	}
	return relation, nil
}

func (d *DB) deleteRelation(ctx context.Context, ac *AccessClaim, userID, relationID uuid.UUID) (*UserRelation, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	var relation UserRelation
	_, err := d.DB.NewUpdate().
		Model(&relation).
		Where("id = ?", relationID).
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Returning("*").
		Exec(ctx, &relation)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoAccess
		}
		return nil, err
	}
	return &relation, nil
}

func (d *DB) InsertUser(ctx context.Context, in *SamnaUser) (*SamnaUser, error) {
	var payload []byte
	err := d.NewRaw(`
		SELECT (claimius.ensure_app_user(?, claimius.get_disciple_app_id(), ?, ?, ?, ?, ?, ?, ?) -> 'samna_user')::jsonb
	`, in.UserID, in.FirstName, in.LastName, in.UserName, in.UserImage, in.Email, in.Phone, in.ExternalID).Scan(ctx, &payload)
	if err != nil {
		return nil, err
	}
	var out SamnaUser
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (d *DB) updateUser(ctx context.Context, ac *AccessClaim, userID uuid.UUID, user *SamnaUser) (*SamnaUser, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	q := d.DB.NewUpdate().
		Model(user).
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("id", "user_id", "app_id", "type", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		Value("app_id", "claimius.get_disciple_app_id()").
		OmitZero().
		Returning("*")
	for _, column := range user.PatchNulls() {
		q = q.Set("? = NULL", bun.Ident(column))
	}
	_, err := q.Exec(ctx, user)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (d *DB) DeleteUser(ctx context.Context, ac *AccessClaim, userID uuid.UUID) (*SamnaUser, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	var user SamnaUser
	_, err := d.DB.NewUpdate().
		Model(&user).
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Returning("*").
		Exec(ctx, &user)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (d *DB) GetUserAudit(ctx context.Context, userID uuid.UUID, requiredAccess ClaimAccess) ([]Audit, error) {
	audits := make([]Audit, 0)
	err := d.NewSelect().
		TableExpr("claimius.get_audit(?, claimius.get_disciple_app_id(), ?) AS audit", userID, requiredAccess).
		Column("audit.*").
		OrderExpr("audit.sa_created_at DESC").
		Scan(ctx, &audits)
	if err != nil {
		return nil, err
	}
	return audits, nil
}

func (d *DB) AssertUserSingleRoot(ctx context.Context, userID uuid.UUID, targetOwnerID uuid.UUID) error {
	if userID == uuid.Nil || targetOwnerID == uuid.Nil {
		return nil
	}
	var conflict bool
	err := d.NewRaw(`
		SELECT EXISTS (
			SELECT 1
			FROM claimius.user_claim uc
			JOIN claimius.claim c ON c.id = uc.claim_id
			JOIN claimius.organization t ON t.id = ?
			WHERE uc.user_id = ?
			  AND uc.app_id = claimius.get_disciple_app_id()
			  AND uc.sa_deleted_at IS NULL
			  AND c.sa_deleted_at IS NULL
			  AND c.sa_root_id <> t.sa_root_id
		)
	`, targetOwnerID, userID).Scan(ctx, &conflict)
	if err != nil {
		return err
	}
	if conflict {
		return ErrRootConflict
	}
	return nil
}

func (d *DB) AssertUserTokenSingleRoot(ctx context.Context, externalID, idClaim string, targetOwnerID uuid.UUID) error {
	if externalID == "" || idClaim == "" || targetOwnerID == uuid.Nil {
		return nil
	}
	var conflict bool
	err := d.NewRaw(`
		SELECT EXISTS (
			SELECT 1
			FROM claimius.user_token ut
			JOIN claimius.organization existing ON existing.id = ut.sa_owner_id
			JOIN claimius.organization target ON target.id = ?
			WHERE ut.external_id = ?
			  AND ut.id_claim = ?
			  AND ut.app_id = claimius.get_disciple_app_id()
			  AND ut.sa_deleted_at IS NULL
			  AND existing.sa_root_id <> target.sa_root_id
		)
	`, targetOwnerID, externalID, idClaim).Scan(ctx, &conflict)
	if err != nil {
		return err
	}
	if conflict {
		return ErrRootConflict
	}
	return nil
}

func (d *DB) CreateOrganizationUser(ctx context.Context, ac *AccessClaim, in *SamnaUser) (*SamnaUser, error) {
	if ac == nil || ac.UserClaimID == nil || ac.ClaimID == nil {
		return nil, ErrNoAccess
	}
	if in == nil {
		return nil, fmt.Errorf("user required")
	}
	if in.OwnerID == nil || *in.OwnerID == uuid.Nil {
		return nil, fmt.Errorf("owner_id required")
	}
	if in.ClaimID == nil || *in.ClaimID == uuid.Nil {
		return nil, fmt.Errorf("claim_id required")
	}
	userJSON, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("marshal user: %w", err)
	}
	return d.callCreateOrganizationUser(ctx, d.DB, ac, *in.OwnerID, userJSON)
}

func (d *DB) callCreateOrganizationUser(ctx context.Context, idb bun.IDB, ac *AccessClaim, ownerID uuid.UUID, userJSON []byte) (*SamnaUser, error) {
	var payload []byte
	err := idb.NewRaw(`
		SELECT claimius.create_organization_user(?, ?, ?, ?::jsonb)
	`, *ac.UserClaimID, *ac.ClaimID, ownerID, string(userJSON)).Scan(ctx, &payload)
	if err != nil {
		return nil, fmt.Errorf("create_organization_user: %w", err)
	}
	var wrapped struct {
		SamnaUser *SamnaUser `json:"samna_user"`
	}
	if err := json.Unmarshal(payload, &wrapped); err != nil {
		return nil, fmt.Errorf("unmarshal create_organization_user: %w", err)
	}
	if wrapped.SamnaUser == nil {
		return nil, fmt.Errorf("create_organization_user returned no samna_user")
	}
	return wrapped.SamnaUser, nil
}

func (d *DB) IssueUserToken(ctx context.Context, ac *AccessClaim, payload SamnaUserToken) (*SamnaUser, error) {
	if ac == nil || ac.UserClaimID == nil || ac.ClaimID == nil {
		return nil, ErrNoAccess
	}
	if payload.Token == "" {
		return nil, fmt.Errorf("token is required")
	}
	if payload.OwnerID == nil || *payload.OwnerID == uuid.Nil {
		return nil, fmt.Errorf("owner_id required")
	}
	if payload.ClaimID == nil || *payload.ClaimID == uuid.Nil {
		return nil, fmt.Errorf("claim_id required")
	}
	parsed, _, err := jwt.NewParser().ParseUnverified(payload.Token, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid token claims")
	}

	idClaim := "sub"
	if payload.IDClaim != nil && *payload.IDClaim != "" {
		idClaim = *payload.IDClaim
	}
	rawID, present := claims[idClaim]
	if !present {
		return nil, fmt.Errorf("id claim missing in token")
	}
	tokenID, ok := rawID.(string)
	if !ok || tokenID == "" {
		return nil, fmt.Errorf("id claim not a string")
	}

	var userID uuid.UUID
	if payload.User != nil && payload.User.UserID != uuid.Nil {
		userID = payload.User.UserID
	} else if parsedID, parseErr := uuid.Parse(tokenID); parseErr == nil {
		userID = parsedID
	}

	externalID := tokenID

	user := SamnaUser{
		UserID:     userID,
		ExternalID: &externalID,
		OwnerID:    payload.OwnerID,
		ClaimID:    payload.ClaimID,
	}
	if payload.User != nil {
		user.FirstName = payload.User.FirstName
		user.LastName = payload.User.LastName
		user.UserName = payload.User.UserName
		user.UserImage = payload.User.UserImage
		user.Phone = payload.User.Phone
		user.Email = payload.User.Email
	}

	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return nil, fmt.Errorf("marshal token claims: %w", err)
	}

	if rootErr := d.AssertUserTokenSingleRoot(ctx, externalID, idClaim, *payload.OwnerID); rootErr != nil {
		return nil, rootErr
	}

	userJSON, err := json.Marshal(user)
	if err != nil {
		return nil, fmt.Errorf("marshal user: %w", err)
	}

	var result *SamnaUser
	err = d.DB.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
		out, terr := d.callCreateOrganizationUser(ctx, tx, ac, *payload.OwnerID, userJSON)
		if terr != nil {
			return terr
		}
		token := UserToken{
			UserID:     out.UserID,
			IDClaim:    idClaim,
			ExternalID: externalID,
			ClaimID:    payload.ClaimID,
			Data:       claimsJSON,
			Type:       "token",
			OwnerID:    *payload.OwnerID,
			CreatedBy:  ac.UserClaimID,
		}
		if _, err := tx.NewInsert().Model(&token).
			On("CONFLICT (app_id, sa_owner_id, id_claim, external_id) WHERE sa_deleted_at IS NULL DO UPDATE").
			Set("user_id = EXCLUDED.user_id").
			Set("claim_id = EXCLUDED.claim_id").
			Set("data = EXCLUDED.data").
			Set("type = EXCLUDED.type").
			Set("sa_updated_at = now()").
			Exec(ctx); err != nil {
			return fmt.Errorf("upsert user_token: %w", err)
		}
		result = out
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (d *DB) UpsertUserExternal(ctx context.Context, ac *AccessClaim, payload SamnaUserExternal) (*SamnaUser, error) {
	if ac == nil || ac.UserClaimID == nil || ac.ClaimID == nil {
		return nil, ErrNoAccess
	}
	if payload.OwnerID == nil || *payload.OwnerID == uuid.Nil {
		return nil, fmt.Errorf("owner_id required")
	}
	if payload.ClaimID == nil || *payload.ClaimID == uuid.Nil {
		return nil, fmt.Errorf("claim_id required")
	}
	if payload.User == nil {
		return nil, fmt.Errorf("user required")
	}
	if payload.User.ExternalID == nil || *payload.User.ExternalID == "" {
		return nil, fmt.Errorf("user.external_id required")
	}

	idClaim := "external"
	if payload.IDClaim != nil && *payload.IDClaim != "" {
		idClaim = *payload.IDClaim
	}
	externalID := *payload.User.ExternalID

	user := *payload.User
	user.OwnerID = payload.OwnerID
	user.ClaimID = payload.ClaimID

	if rootErr := d.AssertUserTokenSingleRoot(ctx, externalID, idClaim, *payload.OwnerID); rootErr != nil {
		return nil, rootErr
	}

	userJSON, err := json.Marshal(user)
	if err != nil {
		return nil, fmt.Errorf("marshal user: %w", err)
	}

	var result *SamnaUser
	err = d.DB.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
		out, terr := d.callCreateOrganizationUser(ctx, tx, ac, *payload.OwnerID, userJSON)
		if terr != nil {
			return terr
		}
		token := UserToken{
			UserID:     out.UserID,
			IDClaim:    idClaim,
			ExternalID: externalID,
			ClaimID:    payload.ClaimID,
			Data:       json.RawMessage(`{}`),
			Type:       "external",
			OwnerID:    *payload.OwnerID,
			CreatedBy:  ac.UserClaimID,
		}
		if _, err := tx.NewInsert().Model(&token).
			On("CONFLICT (app_id, sa_owner_id, id_claim, external_id) WHERE sa_deleted_at IS NULL DO UPDATE").
			Set("user_id = EXCLUDED.user_id").
			Set("claim_id = EXCLUDED.claim_id").
			Set("data = EXCLUDED.data").
			Set("type = EXCLUDED.type").
			Set("sa_updated_at = now()").
			Exec(ctx); err != nil {
			return fmt.Errorf("upsert user_token: %w", err)
		}
		result = out
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

