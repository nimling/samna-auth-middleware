//go:build dbtest

package shared_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	sdb "github.com/nimling/samna-auth-middleware/shared"
	"github.com/uptrace/bun"
)

func countAudit(t *testing.T, db *sdb.DB, objectID uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.NewRaw(`SELECT count(*) FROM claimius.audit WHERE object_id = ?`, objectID).Scan(context.Background(), &n); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	return n
}

func TestWriteAuditCommitsAndRollsBackWithTheChange(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	rolled, kept := uuid.New(), uuid.New()
	undone := errors.New("the change fails")

	err := db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := sdb.WriteAudit(ctx, tx, nil, "update", "test", "claimius.organization", rolled, orgRoot, nil, "rolled back"); err != nil {
			return err
		}
		return undone
	})
	if !errors.Is(err, undone) {
		t.Fatalf("the write must reach the change and fail with it, got %v", err)
	}
	if n := countAudit(t, db, rolled); n != 0 {
		t.Fatalf("an audit row of a change that rolled back must roll back with it, found %d", n)
	}

	if err := db.RunInTx(ctx, &sql.TxOptions{}, func(ctx context.Context, tx bun.Tx) error {
		return sdb.WriteAudit(ctx, tx, nil, "update", "test", "claimius.organization", kept, orgRoot, nil, "committed")
	}); err != nil {
		t.Fatalf("write audit: %v", err)
	}
	if n := countAudit(t, db, kept); n != 1 {
		t.Fatalf("an audit row of a committed change must be there once the change commits, found %d", n)
	}
}
