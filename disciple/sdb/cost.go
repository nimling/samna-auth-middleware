package sdb

import (
	"context"

	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func PutAppCost(ctx context.Context, db *shared.DB, ac *shared.AccessClaim, input *shared.AppCostInput) (*shared.AppCost, error) {
	if ac == nil || ac.UserClaimID == nil {
		return nil, shared.ErrNoAccess
	}
	if err := input.ValidatePost(); err != nil {
		return nil, err
	}
	cost := &shared.AppCost{}
	if err := db.NewInsert().Model(input).Access(ac).Scan(ctx, cost); err != nil {
		return nil, err
	}
	return cost, nil
}
