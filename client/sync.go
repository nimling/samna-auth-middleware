package client

import (
	"context"

	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/sync"
)

func init() {
	db.RegisterSyncFunc(Sync)
}

func Sync(ctx context.Context) error {
	return sync.PullSnapshot(ctx)
}
