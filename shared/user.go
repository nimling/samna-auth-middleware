package shared

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/nimling/samna-auth-middleware/v2/client"
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
		ModelTableExpr("claimius.get_relations(?) AS user_relation", userID).
		ExcludeColumn("sa_deleted_at")
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
	err := d.GetRelationQuery(&relation, userID, relationID).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &relation, nil
}

func (d *DB) GetRelationQuery(relation *UserRelation, userID uuid.UUID, relationID uuid.UUID) *bun.SelectQuery {
	return d.NewSelect().
		Model(relation).
		ModelTableExpr("claimius.get_relations(?, ?::uuid[]) AS user_relation", userID, pq.Array([]uuid.UUID{relationID})).
		ExcludeColumn("sa_deleted_at").
		Limit(1)
}

func (d *DB) insertRelation(ctx context.Context, conn bun.IConn, ac *AccessClaim, relation *UserRelation) (*UserRelation, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	_, err := d.DB.NewInsert().Conn(conn).
		Model(relation).
		ExcludeColumn("id", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		Returning("*").
		Exec(ctx, relation)
	if err != nil {
		return nil, err
	}
	return relation, nil
}

func (d *DB) updateRelation(ctx context.Context, conn bun.IConn, ac *AccessClaim, relationID uuid.UUID, relation *UserRelation) (*UserRelation, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	q := d.DB.NewUpdate().Conn(conn).
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

func (d *DB) deleteRelation(ctx context.Context, conn bun.IConn, ac *AccessClaim, userID, relationID uuid.UUID) (*UserRelation, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	var relation UserRelation
	_, err := d.DB.NewUpdate().Conn(conn).
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

func (d *DB) insertUser(ctx context.Context, conn bun.IConn, ac *AccessClaim, in *SamnaUser) (*SamnaUser, bool, error) {
	if in.OwnerID != nil && *in.OwnerID != uuid.Nil {
		result, err := d.callCreateOrganizationUser(ctx, conn, ac, *in.OwnerID, in, false, nil, nil)
		if err != nil {
			return nil, false, err
		}
		return result.SamnaUser, result.Created, nil
	}
	var payload []byte
	err := d.NewRaw(`
		SELECT (claimius.upsert_app_user(?, ?, ?, ?, ?, ?, ?, ?) -> 'samna_user')::jsonb
	`, in.UserID, in.FirstName, in.LastName, in.UserName, in.Email, in.Phone, in.ExternalID, in.UserImage).Conn(conn).Scan(ctx, &payload)
	if err != nil {
		return nil, false, err
	}
	var out SamnaUser
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, false, err
	}
	return &out, false, nil
}

func (d *DB) updateUser(ctx context.Context, conn bun.IConn, ac *AccessClaim, userID uuid.UUID, user *SamnaUser) (*SamnaUser, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	q := d.DB.NewUpdate().Conn(conn).
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

func (d *DB) DeleteUser(ctx context.Context, conn bun.IConn, ac *AccessClaim, userID uuid.UUID) (*SamnaUser, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	var user SamnaUser
	if err := d.NewSelect().Conn(conn).
		Model(&user).
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx); err != nil {
		return nil, err
	}
	if err := d.NewDelete().Conn(conn).Model(&user).Access(ac).ID(userID).Scan(ctx, &user); err != nil {
		return nil, err
	}
	if user.DeletedAt == nil {
		now := time.Now()
		user.DeletedAt = &now
	}
	return &user, nil
}

func (d *DB) deleteUser(ctx context.Context, conn bun.IConn, ac *AccessClaim, userID uuid.UUID) (*SamnaUser, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	var user SamnaUser
	write := func(ctx context.Context, tx bun.IConn) error {
		if _, err := d.DB.NewUpdate().Conn(tx).
			Model(&user).
			Where("user_id = ?", userID).
			Where("sa_deleted_at IS NULL").
			Set("sa_deleted_at = NOW()").
			Returning("*").
			Exec(ctx, &user); err != nil {
			return err
		}
		if _, err := d.DB.NewRaw(`
			UPDATE claimius.user_token
			SET sa_deleted_at = NOW()
			WHERE user_id = ?
			  AND TRUE
			  AND sa_deleted_at IS NULL
		`, userID).Conn(tx).Exec(ctx); err != nil {
			return err
		}
		return nil
	}
	var err error
	if conn != nil {
		err = write(ctx, conn)
	} else {
		err = d.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error { return write(ctx, tx) })
	}
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

func (d *DB) AssertUserSingleRoot(ctx context.Context, conn bun.IConn, userID uuid.UUID, targetOwnerID uuid.UUID) error {
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
	`, targetOwnerID, userID).Conn(conn).Scan(ctx, &conflict)
	if err != nil {
		return err
	}
	if conflict {
		return ErrRootConflict
	}
	return nil
}

func (d *DB) AssertUserTokenSingleRoot(ctx context.Context, conn bun.IConn, externalID string, targetOwnerID uuid.UUID) error {
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
	`, targetOwnerID, externalID).Conn(conn).Scan(ctx, &conflict)
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
	Created         bool         `json:"created"`
	UserClaim       *UserClaim   `json:"user_claim"`
	VirtualClaim    *Claim       `json:"virtual_claim"`
	ClaimObject     *ClaimObject `json:"claim_object"`
	CallerUserClaim *UserClaim   `json:"caller_user_claim"`
	UserToken       *UserToken   `json:"user_token"`
}

func (d *DB) CreateOrganizationUser(ctx context.Context, conn bun.IConn, ac *AccessClaim, in *SamnaUser) (*SamnaUser, error) {
	if ac == nil || ac.UserClaimID == nil {
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
		if rootErr := d.AssertUserSingleRoot(ctx, conn, in.UserID, *in.OwnerID); rootErr != nil {
			return nil, rootErr
		}
	}
	if err := d.NewInsert().Conn(conn).Model(in).Access(ac).Scan(ctx); err != nil {
		if errors.Is(err, client.ErrConflict) {
			return nil, ErrUserAlreadyExists
		}
		return nil, err
	}
	return in, nil
}

func (d *DB) callCreateOrganizationUser(ctx context.Context, conn bun.IConn, ac *AccessClaim, ownerID uuid.UUID, user *SamnaUser, upsert bool, identity map[string]any, reason *string) (*OrganizationUserResult, error) {
	if ac == nil || ac.UserClaimID == nil {
		return nil, ErrNoAccess
	}
	userJSON, err := json.Marshal(user)
	if err != nil {
		return nil, fmt.Errorf("marshal user: %w", err)
	}
	var identityJSON *string
	if identity != nil {
		raw, err := json.Marshal(identity)
		if err != nil {
			return nil, fmt.Errorf("marshal identity: %w", err)
		}
		s := string(raw)
		identityJSON = &s
	}
	var payload []byte
	err = d.NewRaw(`
		SELECT claimius.insert_organization_user(?, ?, ?::jsonb, ?, ?::jsonb, ?)
	`, *ac.UserClaimID, ownerID, string(userJSON), upsert, identityJSON, reason).Conn(conn).Scan(ctx, &payload)
	if err != nil {
		var pgErr pgdriver.Error
		if errors.As(err, &pgErr) && pgErr.Field('C') == "23505" {
			return nil, ErrUserAlreadyExists
		}
		return nil, fmt.Errorf("insert_organization_user: %w", err)
	}
	var result OrganizationUserResult
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("unmarshal insert_organization_user: %w", err)
	}
	if result.SamnaUser == nil {
		return nil, fmt.Errorf("insert_organization_user returned no samna_user")
	}
	return &result, nil
}

func (d *DB) IssueUserToken(ctx context.Context, conn bun.IConn, ac *AccessClaim, payload SamnaUserToken) (*SamnaUser, bool, error) {
	if ac == nil || ac.UserClaimID == nil {
		return nil, false, ErrNoAccess
	}
	_, _, externalID, _, err := getTokenIdentity(payload)
	if err != nil {
		return nil, false, err
	}
	return d.putUserIdentity(ctx, conn, ac, &payload, *payload.OwnerID, externalID)
}

func (d *DB) UpsertUserExternal(ctx context.Context, conn bun.IConn, ac *AccessClaim, payload SamnaUserExternal) (*SamnaUser, bool, error) {
	if ac == nil || ac.UserClaimID == nil {
		return nil, false, ErrNoAccess
	}
	_, externalID, err := getExternalIdentity(payload)
	if err != nil {
		return nil, false, err
	}
	return d.putUserIdentity(ctx, conn, ac, &payload, *payload.OwnerID, externalID)
}

func (d *DB) putUserIdentity(ctx context.Context, conn bun.IConn, ac *AccessClaim, payload any, ownerID uuid.UUID, externalID string) (*SamnaUser, bool, error) {
	if rootErr := d.AssertUserTokenSingleRoot(ctx, conn, externalID, ownerID); rootErr != nil {
		return nil, false, rootErr
	}
	var user SamnaUser
	q := d.NewInsert().Conn(conn).Model(payload).Access(ac)
	if err := q.Scan(ctx, &user); err != nil {
		return nil, false, err
	}
	return &user, q.Created(), nil
}

func getTokenIdentity(payload SamnaUserToken) (SamnaUser, string, string, json.RawMessage, error) {
	if payload.Token == "" {
		return SamnaUser{}, "", "", nil, fmt.Errorf("token is required")
	}
	if payload.OwnerID == nil || *payload.OwnerID == uuid.Nil {
		return SamnaUser{}, "", "", nil, fmt.Errorf("owner_id required")
	}
	if payload.ClaimID == nil || *payload.ClaimID == uuid.Nil {
		return SamnaUser{}, "", "", nil, fmt.Errorf("claim_id required")
	}
	parsed, _, err := jwt.NewParser().ParseUnverified(payload.Token, jwt.MapClaims{})
	if err != nil {
		return SamnaUser{}, "", "", nil, fmt.Errorf("parse token: %w", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return SamnaUser{}, "", "", nil, fmt.Errorf("invalid token claims")
	}
	idClaim := "sub"
	if payload.IDClaim != nil && *payload.IDClaim != "" {
		idClaim = *payload.IDClaim
	}
	rawID, present := claims[idClaim]
	if !present {
		return SamnaUser{}, "", "", nil, fmt.Errorf("id claim missing in token")
	}
	tokenID, ok := rawID.(string)
	if !ok || tokenID == "" {
		return SamnaUser{}, "", "", nil, fmt.Errorf("id claim not a string")
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
		user.Phone = payload.User.Phone
		user.Email = payload.User.Email
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return SamnaUser{}, "", "", nil, fmt.Errorf("marshal token claims: %w", err)
	}
	return user, idClaim, externalID, claimsJSON, nil
}

func getExternalIdentity(payload SamnaUserExternal) (SamnaUser, string, error) {
	if payload.OwnerID == nil || *payload.OwnerID == uuid.Nil {
		return SamnaUser{}, "", fmt.Errorf("owner_id required")
	}
	if payload.ClaimID == nil || *payload.ClaimID == uuid.Nil {
		return SamnaUser{}, "", fmt.Errorf("claim_id required")
	}
	if payload.User == nil {
		return SamnaUser{}, "", fmt.Errorf("user required")
	}
	if payload.User.ExternalID == nil || *payload.User.ExternalID == "" {
		return SamnaUser{}, "", fmt.Errorf("user.external_id required")
	}
	user := *payload.User
	user.OwnerID = payload.OwnerID
	user.ClaimID = payload.ClaimID
	return user, *payload.User.ExternalID, nil
}

func (d *DB) issueUserToken(ctx context.Context, conn bun.IConn, ac *AccessClaim, payload SamnaUserToken) (*OrganizationUserResult, error) {
	user, idClaim, externalID, claimsJSON, err := getTokenIdentity(payload)
	if err != nil {
		return nil, err
	}
	identity := map[string]any{"external_id": externalID, "id_claim": idClaim, "type": "token", "data": claimsJSON}
	return d.callCreateOrganizationUser(ctx, conn, ac, *payload.OwnerID, &user, true, identity, payload.Reason)
}

func (d *DB) upsertUserExternal(ctx context.Context, conn bun.IConn, ac *AccessClaim, payload SamnaUserExternal) (*OrganizationUserResult, error) {
	user, externalID, err := getExternalIdentity(payload)
	if err != nil {
		return nil, err
	}
	identity := map[string]any{"external_id": externalID, "id_claim": "external", "type": "external", "data": json.RawMessage(`{}`)}
	return d.callCreateOrganizationUser(ctx, conn, ac, *payload.OwnerID, &user, true, identity, payload.Reason)
}

func (d *DB) GetUsersMany(ctx context.Context, userIDs, ids []uuid.UUID, emails, externalIDs []string) ([]SamnaUser, error) {
	users := make([]SamnaUser, 0)
	err := d.NewRaw("SELECT * FROM claimius.get_users(?::uuid[], ?::uuid[], ?::text[], ?::text[])", pq.Array(userIDs), pq.Array(ids), pq.Array(emails), pq.Array(externalIDs)).Scan(ctx, &users)
	if err != nil {
		return nil, err
	}
	return users, nil
}

func (d *DB) GetUserTokens(ctx context.Context, userIDs []uuid.UUID) ([]UserToken, error) {
	tokens := make([]UserToken, 0)
	err := d.NewRaw("SELECT * FROM claimius.get_user_tokens(?::uuid[])", pq.Array(userIDs)).Scan(ctx, &tokens)
	if err != nil {
		return nil, err
	}
	return tokens, nil
}
