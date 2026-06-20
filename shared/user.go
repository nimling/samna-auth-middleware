package shared

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"
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
		Where(UserWhere(userID, requiredAccess))
}

func (d *DB) GetUserClaims(ctx context.Context, userID uuid.UUID) ([]UserClaim, error) {
	claims := make([]UserClaim, 0)
	err := d.NewSelect().
		Model(&claims).
		ExcludeColumn("sa_deleted_at").
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
		ExcludeColumn("id", "user_id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
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
		SELECT (claimius.ensure_app_user(?, ?, ?, ?, ?, ?, ?, ?) -> 'samna_user')::jsonb
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
		ExcludeColumn("id", "user_id", "type", "sa_created_at", "sa_updated_at", "sa_deleted_at").
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
	err := d.DB.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewUpdate().
			Model(&user).
			Where("user_id = ?", userID).
			Where("sa_deleted_at IS NULL").
			Set("sa_deleted_at = NOW()").
			Returning("*").
			Exec(ctx, &user); err != nil {
			return err
		}
		if _, err := tx.NewRaw(`
			UPDATE claimius.user_token
			SET sa_deleted_at = NOW()
			WHERE user_id = ?
			  AND TRUE
			  AND sa_deleted_at IS NULL
		`, userID).Exec(ctx); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (d *DB) GetUserAudit(ctx context.Context, userID uuid.UUID, requiredAccess ClaimAccess) ([]Audit, error) {
	audits := make([]Audit, 0)
	err := d.NewSelect().
		TableExpr("claimius.get_audit(?, ?) AS audit", userID, requiredAccess).
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
			  AND TRUE
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

func (d *DB) AssertUserTokenSingleRoot(ctx context.Context, externalID string, targetOwnerID uuid.UUID) error {
	if externalID == "" || targetOwnerID == uuid.Nil {
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
			  AND TRUE
			  AND ut.sa_deleted_at IS NULL
			  AND existing.sa_root_id <> target.sa_root_id
		)
	`, targetOwnerID, externalID).Scan(ctx, &conflict)
	if err != nil {
		return err
	}
	if conflict {
		return ErrRootConflict
	}
	return nil
}

type OrganizationUserResult struct {
	SamnaUser       *SamnaUser   `json:"samna_user"`
	UserClaim       *UserClaim   `json:"user_claim"`
	VirtualClaim    *Claim       `json:"virtual_claim"`
	ClaimObject     *ClaimObject `json:"claim_object"`
	CallerUserClaim *UserClaim   `json:"caller_user_claim"`
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
	if in.UserID != uuid.Nil {
		if rootErr := d.AssertUserSingleRoot(ctx, in.UserID, *in.OwnerID); rootErr != nil {
			return nil, rootErr
		}
	}
	userJSON, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("marshal user: %w", err)
	}
	result, err := d.callCreateOrganizationUser(ctx, d.DB, ac, *in.OwnerID, userJSON, false)
	if err != nil {
		return nil, err
	}
	return result.SamnaUser, nil
}

func (d *DB) callCreateOrganizationUser(ctx context.Context, idb bun.IDB, ac *AccessClaim, ownerID uuid.UUID, userJSON []byte, upsert bool) (*OrganizationUserResult, error) {
	var payload []byte
	err := idb.NewRaw(`
		SELECT claimius.create_organization_user(?, ?, ?, ?::jsonb, ?)
	`, *ac.UserClaimID, *ac.ClaimID, ownerID, string(userJSON), upsert).Scan(ctx, &payload)
	if err != nil {
		var pgErr pgdriver.Error
		if errors.As(err, &pgErr) && pgErr.Field('C') == "23505" {
			return nil, ErrUserAlreadyExists
		}
		return nil, fmt.Errorf("create_organization_user: %w", err)
	}
	var result OrganizationUserResult
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("unmarshal create_organization_user: %w", err)
	}
	if result.SamnaUser == nil {
		return nil, fmt.Errorf("create_organization_user returned no samna_user")
	}
	return &result, nil
}

func (d *DB) IssueUserToken(ctx context.Context, ac *AccessClaim, payload SamnaUserToken) (*SamnaUser, bool, error) {
	if ac == nil || ac.UserClaimID == nil || ac.ClaimID == nil {
		return nil, false, ErrNoAccess
	}
	if payload.Token == "" {
		return nil, false, fmt.Errorf("token is required")
	}
	if payload.OwnerID == nil || *payload.OwnerID == uuid.Nil {
		return nil, false, fmt.Errorf("owner_id required")
	}
	if payload.ClaimID == nil || *payload.ClaimID == uuid.Nil {
		return nil, false, fmt.Errorf("claim_id required")
	}
	parsed, _, err := jwt.NewParser().ParseUnverified(payload.Token, jwt.MapClaims{})
	if err != nil {
		return nil, false, fmt.Errorf("parse token: %w", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, false, fmt.Errorf("invalid token claims")
	}

	idClaim := "sub"
	if payload.IDClaim != nil && *payload.IDClaim != "" {
		idClaim = *payload.IDClaim
	}
	rawID, present := claims[idClaim]
	if !present {
		return nil, false, fmt.Errorf("id claim missing in token")
	}
	tokenID, ok := rawID.(string)
	if !ok || tokenID == "" {
		return nil, false, fmt.Errorf("id claim not a string")
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
		return nil, false, fmt.Errorf("marshal token claims: %w", err)
	}

	if rootErr := d.AssertUserTokenSingleRoot(ctx, externalID, *payload.OwnerID); rootErr != nil {
		return nil, false, rootErr
	}

	var result *OrganizationUserResult
	var created bool
	err = d.DB.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
		var existingUserID uuid.UUID
		lookupErr := tx.NewRaw(`
			SELECT user_id FROM claimius.user_token
			WHERE TRUE
			  AND sa_owner_id = ?
			  AND external_id = ?
			  AND sa_deleted_at IS NULL
			LIMIT 1
		`, *payload.OwnerID, externalID).Scan(ctx, &existingUserID)
		if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
			return fmt.Errorf("lookup user_token: %w", lookupErr)
		}
		created = existingUserID == uuid.Nil
		if existingUserID != uuid.Nil {
			user.UserID = existingUserID
		}
		userJSON, marshalErr := json.Marshal(user)
		if marshalErr != nil {
			return fmt.Errorf("marshal user: %w", marshalErr)
		}
		out, terr := d.callCreateOrganizationUser(ctx, tx, ac, *payload.OwnerID, userJSON, true)
		if terr != nil {
			return terr
		}
		token := UserToken{
			UserID:     out.SamnaUser.UserID,
			IDClaim:    idClaim,
			ExternalID: externalID,
			ClaimID:    payload.ClaimID,
			Data:       claimsJSON,
			Type:       "token",
			OwnerID:    *payload.OwnerID,
			CreatedBy:  ac.UserClaimID,
		}
		if _, err := tx.NewInsert().Model(&token).
			On("CONFLICT (sa_owner_id, id_claim, external_id) WHERE sa_deleted_at IS NULL DO UPDATE").
			Set("user_id = EXCLUDED.user_id").
			Set("id_claim = EXCLUDED.id_claim").
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
		return nil, false, err
	}
	return result.SamnaUser, created, nil
}

func (d *DB) UpsertUserExternal(ctx context.Context, ac *AccessClaim, payload SamnaUserExternal) (*SamnaUser, bool, error) {
	if ac == nil || ac.UserClaimID == nil || ac.ClaimID == nil {
		return nil, false, ErrNoAccess
	}
	if payload.OwnerID == nil || *payload.OwnerID == uuid.Nil {
		return nil, false, fmt.Errorf("owner_id required")
	}
	if payload.ClaimID == nil || *payload.ClaimID == uuid.Nil {
		return nil, false, fmt.Errorf("claim_id required")
	}
	if payload.User == nil {
		return nil, false, fmt.Errorf("user required")
	}
	if payload.User.ExternalID == nil || *payload.User.ExternalID == "" {
		return nil, false, fmt.Errorf("user.external_id required")
	}

	idClaim := "external"
	externalID := *payload.User.ExternalID

	user := *payload.User
	user.OwnerID = payload.OwnerID
	user.ClaimID = payload.ClaimID

	if rootErr := d.AssertUserTokenSingleRoot(ctx, externalID, *payload.OwnerID); rootErr != nil {
		return nil, false, rootErr
	}

	var result *OrganizationUserResult
	var created bool
	err := d.DB.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
		var existingUserID uuid.UUID
		lookupErr := tx.NewRaw(`
			SELECT user_id FROM claimius.user_token
			WHERE TRUE
			  AND sa_owner_id = ?
			  AND external_id = ?
			  AND sa_deleted_at IS NULL
			LIMIT 1
		`, *payload.OwnerID, externalID).Scan(ctx, &existingUserID)
		if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
			return fmt.Errorf("lookup user_token: %w", lookupErr)
		}
		created = existingUserID == uuid.Nil
		if existingUserID != uuid.Nil {
			user.UserID = existingUserID
		}
		userJSON, marshalErr := json.Marshal(user)
		if marshalErr != nil {
			return fmt.Errorf("marshal user: %w", marshalErr)
		}
		out, terr := d.callCreateOrganizationUser(ctx, tx, ac, *payload.OwnerID, userJSON, true)
		if terr != nil {
			return terr
		}
		token := UserToken{
			UserID:     out.SamnaUser.UserID,
			IDClaim:    idClaim,
			ExternalID: externalID,
			ClaimID:    payload.ClaimID,
			Data:       json.RawMessage(`{}`),
			Type:       "external",
			OwnerID:    *payload.OwnerID,
			CreatedBy:  ac.UserClaimID,
		}
		if _, err := tx.NewInsert().Model(&token).
			On("CONFLICT (sa_owner_id, id_claim, external_id) WHERE sa_deleted_at IS NULL DO UPDATE").
			Set("user_id = EXCLUDED.user_id").
			Set("id_claim = EXCLUDED.id_claim").
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
		return nil, false, err
	}
	return result.SamnaUser, created, nil
}

