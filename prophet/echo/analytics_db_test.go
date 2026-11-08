//go:build dbtest

package echo_test

import (
	"context"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/analytics"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
)

func TestAnalyticsFoldRunsOnOneReplica(t *testing.T) {
	db, err := psdb.Init(echo.New().Logger)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()
	tryLock := func() bool {
		var held bool
		if err := conn.QueryRowContext(t.Context(), "SELECT pg_try_advisory_lock(hashtextextended('claimius.analytics_fold', 0))").Scan(&held); err != nil {
			t.Fatalf("try lock: %v", err)
		}
		if held {
			if _, err := conn.ExecContext(t.Context(), "SELECT pg_advisory_unlock(hashtextextended('claimius.analytics_fold', 0))"); err != nil {
				t.Fatalf("unlock: %v", err)
			}
		}
		return held
	}
	ctx, cancel := context.WithCancel(t.Context())
	if err := analytics.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for tryLock() {
		if time.Now().After(deadline) {
			t.Fatalf("the fold never took the leader lock")
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	select {
	case <-analytics.Done():
	case <-time.After(15 * time.Second):
		t.Fatalf("Done not closed after cancel")
	}
	if !tryLock() {
		t.Fatalf("the fold kept the leader lock after the end")
	}
}
