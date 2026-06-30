package db

import (
	"context"

	"github.com/google/uuid"
)

func (d *DB) InsertRootOrganization(ctx context.Context, ac *AccessClaim, org *Organization) (*Organization, error) {
	if ac == nil || ac.UserClaimID == nil {
		return nil, ErrNoAccess
	}
	org.ID = uuid.New()
	org.OwnerID = org.ID
	org.RootID = org.ID
	org.CreatedBy = *ac.UserClaimID
	org.Level = 0
	if err := d.NewInsert().Model(org).Access(ac).Scan(ctx, org); err != nil {
		return nil, err
	}
	return org, nil
}
