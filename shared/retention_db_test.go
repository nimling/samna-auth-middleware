//go:build dbtest

package shared_test

import (
	"context"
	"testing"

	sdb "github.com/nimling/samna-auth-middleware/v2/shared"
)

func TestPruneRunsEveryGroupAndTheHostsOwn(t *testing.T) {
	db := database(t)
	ctx := context.Background()

	if _, err := db.Prune(ctx, sdb.Retention{"size_mb": 0}); err != nil {
		t.Fatalf("prune with no size limit: %v", err)
	}
	if _, err := db.Prune(ctx, sdb.Retention{"audit_days": 0, "notification_days": 0, "token_days": 0}); err != nil {
		t.Fatalf("prune with every group off: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE FUNCTION public.prune(p_settings JSONB) RETURNS BIGINT AS $$ SELECT (p_settings ->> 'size_mb')::BIGINT $$ LANGUAGE sql`); err != nil {
		t.Fatalf("host prune: %v", err)
	}
	var n int64
	if err := tx.NewRaw(`SELECT claimius.prune('{"size_mb": 1000000}'::jsonb)`).Scan(ctx, &n); err != nil {
		t.Fatalf("prune through the host's own: %v", err)
	}
	if n != 1000000 {
		t.Fatalf("claimius.prune adds what public.prune returns, got %d", n)
	}
}
