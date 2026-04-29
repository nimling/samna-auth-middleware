package db

import (
	"context"

	"github.com/google/uuid"
)

func (d *DB) GetUser(ctx context.Context, userID uuid.UUID, relations ...RelationFunc) (*SamnaUser, error) {
	var user SamnaUser
	q := d.NewSelect().
		Model(&user).
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		Limit(1)
	q = ApplyRelations(q, relations)
	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (d *DB) GetUsers(ctx context.Context, userID uuid.UUID, accessLevel ClaimAccessLevel) ([]SamnaUser, error) {
	users := make([]SamnaUser, 0)
	err := d.NewSelect().
		Model(&users).
		Where("samna_user.user_id IN (SELECT user_id FROM claimius.get_users(?, claimius.get_disciple_app_id(), ?))", userID, accessLevel).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return users, nil
}

func (d *DB) GetUserClaims(ctx context.Context, userID uuid.UUID) ([]UserClaim, error) {
	claims := make([]UserClaim, 0)
	err := d.NewSelect().
		Model(&claims).
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
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return relations, nil
}

func (d *DB) GetRelation(ctx context.Context, userID uuid.UUID, objectID uuid.UUID) ([]UserRelation, error) {
	relations := make([]UserRelation, 0)
	err := d.NewSelect().
		Model(&relations).
		Where("user_id = ?", userID).
		Where("object_id = ?", objectID).
		Where("sa_deleted_at IS NULL").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return relations, nil
}

func (d *DB) PostRelation(ctx context.Context, in *UserRelation) error {
	in.ID = uuid.UUID{}
	q := d.NewInsert().Model(in).ExcludeColumn("id").Returning("*")
	_, err := q.Exec(ctx)
	return err
}

func (d *DB) PutRelation(ctx context.Context, relationID uuid.UUID, in *UserRelation) error {
	_, err := d.NewUpdate().
		Model(in).
		Where("id = ?", relationID).
		Where("sa_deleted_at IS NULL").
		OmitZero().
		Exec(ctx)
	return err
}

func (d *DB) DeleteRelation(ctx context.Context, relationID uuid.UUID) error {
	_, err := d.NewUpdate().
		Model((*UserRelation)(nil)).
		Set("sa_deleted_at = NOW()").
		Where("id = ?", relationID).
		Where("sa_deleted_at IS NULL").
		Exec(ctx)
	return err
}

func (d *DB) PostUser(ctx context.Context, userID uuid.UUID, in SamnaUser) (*SamnaUser, error) {
	_, err := d.NewRaw(`
		SELECT claimius.ensure_app_user(?, claimius.get_disciple_app_id(), ?, ?, ?, ?, ?, ?)
	`, userID, in.FirstName, in.LastName, in.UserName, in.UserImage, in.Email, in.Phone).Exec(ctx)
	if err != nil {
		return nil, err
	}
	return d.GetUser(ctx, userID)
}

func (d *DB) PutUser(ctx context.Context, userID uuid.UUID, user *SamnaUser) error {
	_, err := d.NewUpdate().
		Model(user).
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		OmitZero().
		Exec(ctx)
	return err
}

func (d *DB) DeleteUser(ctx context.Context, userID uuid.UUID) error {
	_, err := d.NewUpdate().
		Model((*SamnaUser)(nil)).
		Set("sa_deleted_at = NOW()").
		Where("user_id = ?", userID).
		Where("app_id = claimius.get_disciple_app_id()").
		Where("sa_deleted_at IS NULL").
		Exec(ctx)
	return err
}

func (d *DB) GetUserAudit(ctx context.Context, userID uuid.UUID) ([]Audit, error) {
	audits := make([]Audit, 0)
	err := d.NewSelect().
		Model(&audits).
		Where("object_type = ?", OBJECT_TYPE_USER).
		Where("object_id = ?", userID).
		OrderExpr("sa_created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return audits, nil
}
