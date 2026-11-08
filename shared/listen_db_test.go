//go:build dbtest

package shared_test

import (
	"context"
	"testing"
	"time"

	"github.com/lib/pq"
)

func countListeners(t *testing.T, ctx context.Context) int {
	t.Helper()
	db := database(t)
	defer db.Close()
	var n int
	if err := db.NewRaw(`SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND query ILIKE 'LISTEN "claimius_listen_test_%'`).Scan(ctx, &n); err != nil {
		t.Fatalf("count listeners: %v", err)
	}
	return n
}

func getNotification(t *testing.T, ch <-chan *pq.Notification, channel string) {
	t.Helper()
	select {
	case n := <-ch:
		if n == nil || n.Channel != channel || n.Extra != "hello" {
			t.Fatalf("notification = %+v, want %s hello", n, channel)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no notification on %s", channel)
	}
}

func TestListenCarriesEveryChannelOnOneConnection(t *testing.T) {
	db := database(t)
	defer db.Close()
	ctx, cancel := context.WithCancel(t.Context())
	a, err := db.Listen(ctx, "claimius_listen_test_a")
	if err != nil {
		t.Fatalf("listen a: %v", err)
	}
	b, err := db.Listen(ctx, "claimius_listen_test_b")
	if err != nil {
		t.Fatalf("listen b: %v", err)
	}
	again, err := db.Listen(ctx, "claimius_listen_test_a")
	if err != nil {
		t.Fatalf("listen a again: %v", err)
	}
	if n := countListeners(t, t.Context()); n != 1 {
		t.Fatalf("listening connections = %d, want 1", n)
	}
	for _, channel := range []string{"claimius_listen_test_a", "claimius_listen_test_b"} {
		if _, err := db.ExecContext(t.Context(), "SELECT pg_notify(?, 'hello')", channel); err != nil {
			t.Fatalf("notify %s: %v", channel, err)
		}
	}
	getNotification(t, a, "claimius_listen_test_a")
	getNotification(t, again, "claimius_listen_test_a")
	getNotification(t, b, "claimius_listen_test_b")
	cancel()
	for _, ch := range []<-chan *pq.Notification{a, b, again} {
		select {
		case _, ok := <-ch:
			for ok {
				_, ok = <-ch
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("subscription not closed after cancel")
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for countListeners(t, t.Context()) != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("listening connection still open after every subscriber ended")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
