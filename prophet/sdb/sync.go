package sdb

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func GetSyncConsumers(ctx context.Context, appID uuid.UUID) ([]shared.SyncConsumer, error) {
	consumers := make([]shared.SyncConsumer, 0)
	if err := instance.NewRaw("SELECT * FROM claimius.get_sync_consumers(?)", appID).Scan(ctx, &consumers); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return consumers, nil
}
