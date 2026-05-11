package db

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var (
	ErrNoAccess       = errors.New("no access")
	ErrInternalServer = errors.New("internal server error")
)

func (d *DB) MutationAccess(ctx context.Context, userID, objectID uuid.UUID, objectType string, requiredAccess ClaimAccess) (*AccessClaim, error) {
	uc, err := d.GetAccess(ctx, userID, objectID, objectType, requiredAccess)
	if err != nil && !errors.Is(err, ErrNoAccess) {
		return nil, err
	}
	if uc != nil {
		return uc, nil
	}
	uc, err = d.GetDirectAccess(ctx, userID, objectID, objectType)
	if err != nil && !errors.Is(err, ErrNoAccess) {
		return nil, err
	}
	if uc == nil {
		return nil, ErrNoAccess
	}
	if (uc.Access & int(requiredAccess)) != int(requiredAccess) {
		return nil, ErrNoAccess
	}
	return uc, nil
}
