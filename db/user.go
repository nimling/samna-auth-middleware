package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

func (d *DB) GetUserByID(ctx context.Context, userID uuid.UUID, relations ...RelationFunc) (*SamnaUser, error) {
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
		Where("samna_user.id IN (SELECT id FROM samna_auth.get_users(?, ?))", userID, accessLevel).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return users, nil
}

func (d *DB) GetUserClaims(ctx context.Context, userID uuid.UUID) ([]ActiveClaim, error) {
	var claims []ActiveClaim
	err := d.NewSelect().
		Model(&claims).
		Relation("Objects", func(q *bun.SelectQuery) *bun.SelectQuery {
			return q.Where("sa_deleted_at IS NULL")
		}).
		Where("user_id = ?", userID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	return claims, nil
}

func (d *DB) UpdateUser(ctx context.Context, userID uuid.UUID, user *SamnaUser) error {
	_, err := d.NewUpdate().
		Model(user).
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		OmitZero().
		Exec(ctx)
	return err
}
