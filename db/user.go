package db

import (
	"context"

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

func (d *DB) GetUserRelations(ctx context.Context, userID uuid.UUID) ([]UserRelation, error) {
	relations := make([]UserRelation, 0)
	err := d.NewSelect().
		Model(&relations).
		ExcludeColumn("sa_deleted_at").
		ExcludeColumn("app_id").
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return relations, nil
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

func (d *DB) PostRelation(ctx context.Context, ac *AccessClaim, relation *UserRelation) (*UserRelation, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	_, err := d.NewInsert().
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

func (d *DB) PutRelation(ctx context.Context, ac *AccessClaim, relationID uuid.UUID, relation *UserRelation) (*UserRelation, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	_, err := d.NewUpdate().
		Model(relation).
		Where("id = ?", relationID).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("app_id").
		OmitZero().
		Returning("*").
		Exec(ctx, relation)
	if err != nil {
		return nil, err
	}
	return relation, nil
}

func (d *DB) DeleteRelation(ctx context.Context, ac *AccessClaim, relationID uuid.UUID) (*UserRelation, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	var relation UserRelation
	_, err := d.NewUpdate().
		Model(&relation).
		Where("id = ?", relationID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Returning("*").
		Exec(ctx, &relation)
	if err != nil {
		return nil, err
	}
	return &relation, nil
}

func (d *DB) PostUser(ctx context.Context, in SamnaUser) (*SamnaUser, error) {
	var userID uuid.UUID
	err := d.NewRaw(`
		SELECT (claimius.ensure_app_user(?, claimius.get_disciple_app_id(), ?, ?, ?, ?, ?, ?, ?) -> 'samna_user' ->> 'user_id')::uuid
	`, in.UserID, in.FirstName, in.LastName, in.UserName, in.UserImage, in.Email, in.Phone, in.ExternalID).Scan(ctx, &userID)
	if err != nil {
		return nil, err
	}
	return d.GetUser(ctx, userID, userID)
}

func (d *DB) PutUser(ctx context.Context, ac *AccessClaim, userID uuid.UUID, user *SamnaUser) (*SamnaUser, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	_, err := d.NewUpdate().
		Model(user).
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		Value("app_id", "claimius.get_disciple_app_id()").
		OmitZero().
		Returning("*").
		Exec(ctx, user)
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
	_, err := d.NewUpdate().
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
