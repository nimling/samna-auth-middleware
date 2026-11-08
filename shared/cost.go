package shared

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"
)

type AppCostBucket struct {
	AppID       uuid.UUID `bun:"app_id" json:"app_id"`
	Module      string    `bun:"module" json:"module"`
	PeriodStart time.Time `bun:"period_start" json:"period_start"`
	Unit        string    `bun:"unit" json:"unit"`
	Currency    string    `bun:"currency" json:"currency"`
	Quantity    float64   `bun:"quantity" json:"quantity"`
	Amount      float64   `bun:"amount" json:"amount"`
	Hours       int64     `bun:"hours" json:"hours"`
}

func GetAppCost(ctx context.Context, db *DB, userID, appID uuid.UUID, module string, from, to time.Time, granularity string) ([]AppCostBucket, error) {
	var moduleArg *string
	if module != "" {
		moduleArg = &module
	}
	var fromArg, toArg *time.Time
	if !from.IsZero() {
		fromArg = &from
	}
	if !to.IsZero() {
		toArg = &to
	}
	if granularity == "" {
		granularity = "hour"
	}
	buckets := make([]AppCostBucket, 0)
	q := db.NewRaw("SELECT * FROM claimius.get_app_cost(?, ?, ?, ?, ?)", userID, moduleArg, fromArg, toArg, granularity)
	if db.Prophet {
		q = db.NewRaw("SELECT * FROM claimius.get_app_cost(?, ?, ?, ?, ?, ?)", userID, appID, moduleArg, fromArg, toArg, granularity)
	}
	if err := q.Scan(ctx, &buckets); err != nil {
		return nil, err
	}
	return buckets, nil
}

func (d *DB) deleteAppCost(ctx context.Context, conn bun.IConn, ac *AccessClaim, costID uuid.UUID) (*AppCost, error) {
	if ac == nil {
		return nil, ErrNoAccess
	}
	var cost AppCost
	_, err := d.DB.NewUpdate().Conn(conn).
		Model(&cost).
		Where("id = ?", costID).
		Where("sa_deleted_at IS NULL").
		Set("sa_deleted_at = NOW()").
		Returning("*").
		Exec(ctx, &cost)
	if err != nil {
		return nil, err
	}
	return &cost, nil
}
