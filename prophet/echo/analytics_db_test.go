//go:build dbtest

package echo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/analytics"
	"github.com/nimling/samna-auth-middleware/v2/notification"
	psam "github.com/nimling/samna-auth-middleware/v2/prophet/sam"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
)

func TestProphetInitRunsNotificationAnalyticsAndPeerSync(t *testing.T) {
	t.Setenv("SAUTH_DEBUG_USER_ID", "")
	t.Setenv("SAUTH_DEBUG_CLIENTS", "")
	db, err := psdb.Init(echo.New().Logger)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	var peerID uuid.UUID
	if err := db.NewRaw(`
		INSERT INTO claimius.prophet_peer (prophet, endpoint, app_slug, app_secret, status, sa_created_by)
		VALUES ('gotest-peer', 'http://127.0.0.1:1', ?, 'secret', 'suspended', ?) RETURNING id
	`, "gotest-peer-"+uuid.NewString(), grantUser).Scan(t.Context(), &peerID); err != nil {
		t.Fatalf("peer row: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.ExecContext(context.Background(), `DELETE FROM claimius.prophet_peer WHERE id = ?`, peerID); err != nil {
			t.Errorf("delete peer row: %v", err)
		}
	})
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()
	tryLock := func(key string) bool {
		var held bool
		if err := conn.QueryRowContext(t.Context(), "SELECT pg_try_advisory_lock(hashtextextended(?, 0))", key).Scan(&held); err != nil {
			t.Fatalf("try lock: %v", err)
		}
		if held {
			if _, err := conn.ExecContext(t.Context(), "SELECT pg_advisory_unlock(hashtextextended(?, 0))", key); err != nil {
				t.Fatalf("unlock: %v", err)
			}
		}
		return held
	}
	locks := []string{"claimius.analytics_fold", "claimius.prophet_peer:" + peerID.String()}
	ctx, cancel := context.WithCancel(t.Context())
	if err := psam.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := psam.Init(ctx); !errors.Is(err, psam.ErrStarted) {
		t.Fatalf("second init = %v, want ErrStarted", err)
	}
	for _, key := range locks {
		deadline := time.Now().Add(15 * time.Second)
		for tryLock(key) {
			if time.Now().After(deadline) {
				t.Fatalf("%s never took its leader lock", key)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	for name, done := range map[string]<-chan struct{}{"notification": notification.Done(), "analytics": analytics.Done()} {
		select {
		case <-done:
			t.Fatalf("%s did not start with sam.Init", name)
		default:
		}
	}
	cancel()
	select {
	case <-psam.Done():
	case <-time.After(15 * time.Second):
		t.Fatalf("Done not closed after cancel")
	}
	for name, done := range map[string]<-chan struct{}{"notification": notification.Done(), "analytics": analytics.Done()} {
		select {
		case <-done:
		default:
			t.Fatalf("sam.Done closed before %s ended", name)
		}
	}
	for _, key := range locks {
		if !tryLock(key) {
			t.Fatalf("%s kept its leader lock after the end", key)
		}
	}
}
