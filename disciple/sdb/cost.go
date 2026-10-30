package sdb

import (
	"context"
	"time"

	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func PutAppCost(ctx context.Context, db *shared.DB, ac *shared.AccessClaim, row *shared.AppCost) error {
	if ac == nil || ac.UserClaimID == nil {
		return shared.ErrNoAccess
	}
	hour := row.PeriodStart.UTC()
	row.PeriodStart = time.Date(hour.Year(), hour.Month(), hour.Day(), hour.Hour(), 0, 0, 0, time.UTC)
	row.OwnerID = ac.OwnerID
	row.CreatedBy = ac.UserClaimID
	row.DeletedAt = nil
	if len(row.Detail) == 0 {
		row.Detail = []byte(`{}`)
	}
	return db.NewInsert().Model(row).Access(ac).
		On("CONFLICT (module, period_start) WHERE sa_deleted_at IS NULL DO UPDATE").
		Set("quantity = EXCLUDED.quantity").
		Set("unit = EXCLUDED.unit").
		Set("amount = EXCLUDED.amount").
		Set("currency = EXCLUDED.currency").
		Set("detail = EXCLUDED.detail").
		Set("sa_created_by = EXCLUDED.sa_created_by").
		Returning("*").
		Scan(ctx)
}
