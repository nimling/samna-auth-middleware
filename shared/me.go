package shared

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

const MeContextKey = "user_id"

func meUserID(ctx echo.Context) (uuid.UUID, error) {
	if id, ok := ctx.Get(MeContextKey).(uuid.UUID); ok && id != uuid.Nil {
		return id, nil
	}
	return uuid.Nil, fmt.Errorf("me: no authenticated user in context")
}

func (d *DB) GetMe(ctx echo.Context) (*SamnaUser, error) {
	userID, err := meUserID(ctx)
	if err != nil {
		return nil, err
	}
	return d.GetUser(ctx.Request().Context(), userID, userID)
}

func (d *DB) UpdateMe(ctx echo.Context, user *SamnaUser) (*SamnaUser, error) {
	userID, err := meUserID(ctx)
	if err != nil {
		return nil, err
	}
	ac, err := d.MutationAccess(ctx.Request().Context(), userID, userID, OBJECT_TYPE_USER, ACCESS_WRITE)
	if err != nil {
		return nil, err
	}
	if err := d.NewUpdate().Model(user).Access(ac).ID(userID).Scan(ctx.Request().Context(), user); err != nil {
		return nil, err
	}
	return user, nil
}

func (d *DB) GetMeClaims(ctx echo.Context) ([]Claim, error) {
	userID, err := meUserID(ctx)
	if err != nil {
		return nil, err
	}
	with, err := QueryWithObject(ctx)
	if err != nil {
		return nil, err
	}
	claims := make([]Claim, 0)
	q := d.GetClaimsQuery(&claims, userID, ACCESS_NONE)
	for _, w := range with {
		switch w {
		case OBJECT_TYPE_CLAIM_OBJECT:
			q = q.Relation(RelationClaimObjects())
		case OBJECT_TYPE_USER_CLAIM:
			q = q.Relation(RelationClaimUsers())
		case OBJECT_TYPE_ORGANIZATION:
			q = q.Relation(RelationOwner())
		default:
			return nil, fmt.Errorf("unsupported with object: %s", w)
		}
	}
	if err := q.Scan(ctx.Request().Context()); err != nil {
		return nil, err
	}
	return claims, nil
}

func (d *DB) GetMeRelations(ctx echo.Context) ([]UserRelation, error) {
	userID, err := meUserID(ctx)
	if err != nil {
		return nil, err
	}
	return d.GetUserRelations(ctx.Request().Context(), userID)
}

func (d *DB) GetMeOrganizations(ctx echo.Context) ([]Organization, error) {
	userID, err := meUserID(ctx)
	if err != nil {
		return nil, err
	}
	with, err := QueryWithObject(ctx)
	if err != nil {
		return nil, err
	}
	orgs := make([]Organization, 0)
	q := d.GetOrganizationsQuery(&orgs, userID, ACCESS_NONE)
	for _, w := range with {
		switch w {
		case OBJECT_TYPE_ORGANIZATION:
			q = q.Relation(RelationParent())
		default:
			return nil, fmt.Errorf("unsupported with object: %s", w)
		}
	}
	if err := q.Scan(ctx.Request().Context()); err != nil {
		return nil, err
	}
	return orgs, nil
}

func (d *DB) GetMeLocations(ctx echo.Context) ([]Location, error) {
	userID, err := meUserID(ctx)
	if err != nil {
		return nil, err
	}
	with, err := QueryWithObject(ctx)
	if err != nil {
		return nil, err
	}
	locs := make([]Location, 0)
	q := d.GetLocationsQuery(&locs, userID, ACCESS_READ)
	for _, w := range with {
		switch w {
		case OBJECT_TYPE_ORGANIZATION:
			q = q.Relation(RelationOwner())
		default:
			return nil, fmt.Errorf("unsupported with object: %s", w)
		}
	}
	if err := q.Scan(ctx.Request().Context()); err != nil {
		return nil, err
	}
	return locs, nil
}

func (d *DB) GetMeAssets(ctx echo.Context) ([]Asset, error) {
	userID, err := meUserID(ctx)
	if err != nil {
		return nil, err
	}
	return d.GetUserAssets(ctx.Request().Context(), userID)
}

func (d *DB) GetMeAudit(ctx echo.Context) ([]Audit, error) {
	userID, err := meUserID(ctx)
	if err != nil {
		return nil, err
	}
	return d.GetUserAudit(ctx.Request().Context(), userID, ACCESS_READ)
}
