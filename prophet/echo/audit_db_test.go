//go:build dbtest

package echo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	psdb "github.com/nimling/samna-auth-middleware/prophet/sdb"
	"github.com/uptrace/bun"
)

func TestWriteAuditStampsTheAppInTheChangeTransaction(t *testing.T) {
	appID, _ := getGrantFixture(t)
	ctx := context.Background()
	rolled, kept := uuid.New(), uuid.New()
	undone := errors.New("the change fails")

	err := psdb.Instance().RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := psdb.Write(ctx, tx, appID, "update", "test", "claimius.organization", rolled, rolled, nil, "rolled back"); err != nil {
			return err
		}
		return undone
	})
	if !errors.Is(err, undone) {
		t.Fatalf("the write must reach the change and fail with it, got %v", err)
	}

	if err := psdb.Instance().RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		return psdb.Write(ctx, tx, appID, "update", "test", "claimius.organization", kept, kept, nil, "committed")
	}); err != nil {
		t.Fatalf("write audit: %v", err)
	}

	var rows []uuid.UUID
	if err := psdb.Instance().NewRaw(`SELECT app_id FROM claimius.audit WHERE object_id IN (?, ?)`, rolled, kept).
		Scan(ctx, &rows); err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if len(rows) != 1 || rows[0] != appID {
		t.Fatalf("only the committed change leaves its row, stamped with the app, got %v", rows)
	}
}
