//go:build dbtest

package echo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	sdb "github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/v2/notification"
)

func TestNotificationCancelDrainsTheQueueAndClosesDone(t *testing.T) {
	db, err := sdb.Init(echo.New().Logger)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	user := uuid.New()
	if _, err := db.NewRaw(`INSERT INTO claimius.samna_user (user_id) VALUES (?)`, user).Exec(t.Context()); err != nil {
		t.Fatalf("user: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	if err := notification.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := notification.Init(ctx); !errors.Is(err, notification.ErrStarted) {
		t.Fatalf("second init = %v, want ErrStarted", err)
	}
	const queued = 50
	for i := 0; i < queued; i++ {
		notification.WriteUser(notification.Notification{Title: "drain", Type: "info"}, user)
	}
	cancel()
	select {
	case <-notification.Done():
	case <-time.After(15 * time.Second):
		t.Fatalf("Done not closed after cancel")
	}
	count := func() (n int) {
		if err := db.NewRaw(`SELECT count(*) FROM claimius.notification WHERE user_id = ?`, user).Scan(context.Background(), &n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	if n := count(); n != queued {
		t.Fatalf("notifications written = %d, want %d", n, queued)
	}
	notification.WriteUser(notification.Notification{Title: "late", Type: "info"}, user)
	if n := count(); n != queued {
		t.Fatalf("a write after the end was written: %d", n)
	}
	again, stop := context.WithCancel(t.Context())
	if err := notification.Init(again); err != nil {
		t.Fatalf("init after the end: %v", err)
	}
	stop()
	<-notification.Done()
}
