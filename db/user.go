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
	var users []SamnaUser
	err := d.NewSelect().
		Model(&users).
		Where("samna_user.user_id IN (SELECT user_id FROM claimius.get_users(?, "+appIDExpr+", ?))", userID, appSlug(), accessLevel).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return users, nil
}

func (d *DB) GetUserClaims(ctx context.Context, userID uuid.UUID) ([]UserClaim, error) {
	var claims []UserClaim
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
	var relations []UserRelation
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
	var relations []UserRelation
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

func (d *DB) PutUser(ctx context.Context, userID uuid.UUID, user *SamnaUser) error {
	_, err := d.NewUpdate().
		Model(user).
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		OmitZero().
		Exec(ctx)
	return err
}

func (d *DB) GetUserAudit(ctx context.Context, userID uuid.UUID) ([]Audit, error) {
	var audits []Audit
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
