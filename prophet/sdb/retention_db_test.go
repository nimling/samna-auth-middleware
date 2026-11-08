//go:build dbtest

package sdb_test

import (
	"context"
	"testing"

	"github.com/labstack/echo/v4"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func TestPruneRunsEveryProphetTable(t *testing.T) {
	db, err := psdb.Init(echo.New().Logger)
	if err != nil {
		t.Fatalf("prophet database: %v", err)
	}
	ctx := context.Background()
	if _, err := db.Prune(ctx, shared.Retention{"size_mb": 0}); err != nil {
		t.Fatalf("prune every prophet table with no size limit: %v", err)
	}
	if _, err := db.Prune(ctx, shared.Retention{"analytics_event_days": 1, "token_size_mb": 0, "audit_days": 0}); err != nil {
		t.Fatalf("prune with days per table and size per kind: %v", err)
	}
}
