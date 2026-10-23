//go:build dbtest

package echo_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	sam "github.com/nimling/samna-auth-middleware/v2/disciple/sam"
	sdb "github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/v2/notification"
)

func TestNotificationCancelDrainsTheQueueAndClosesDone(t *testing.T) {
	t.Setenv("SAUTH_DEBUG_USER_ID", uuid.NewString())
	db, err := sdb.Init(echo.New().Logger)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	user := uuid.New()
	if _, err := db.NewRaw(`INSERT INTO claimius.samna_user (user_id) VALUES (?)`, user).Exec(t.Context()); err != nil {
		t.Fatalf("user: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	if err := sam.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := sam.Init(ctx); !errors.Is(err, sam.ErrStarted) {
		t.Fatalf("second init = %v, want ErrStarted", err)
	}
	const queued = 50
	for i := 0; i < queued; i++ {
		notification.WriteUser(notification.Notification{Title: "drain", Type: "info"}, user)
	}
	cancel()
	select {
	case <-sam.Done():
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
	if err := sam.Init(again); err != nil {
		t.Fatalf("init after the end: %v", err)
	}
	stop()
	<-sam.Done()
}

func TestEndStreamsEndsAnOpenStreamBeforeTheRunnerEnds(t *testing.T) {
	t.Setenv("SAUTH_DEBUG_USER_ID", uuid.NewString())
	if _, err := sdb.Init(echo.New().Logger); err != nil {
		t.Fatalf("connect: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := sam.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	e := echo.New()
	e.GET("/", func(c echo.Context) error { return notification.Stream(c, uuid.New()) })
	server := httptest.NewServer(e)
	defer server.Close()
	res, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("stream = %d", res.StatusCode)
	}
	ended := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, res.Body)
		ended <- err
	}()
	select {
	case <-ended:
		t.Fatalf("the stream ended before EndStreams")
	case <-time.After(300 * time.Millisecond):
	}
	start := time.Now()
	notification.EndStreams()
	select {
	case err := <-ended:
		if err != nil {
			t.Fatalf("stream body: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("the stream is still open 2s after EndStreams")
	}
	select {
	case <-sam.Done():
		t.Fatalf("the runner ended with the streams")
	default:
	}
	shutdown, stop := context.WithTimeout(t.Context(), 2*time.Second)
	defer stop()
	if err := server.Config.Shutdown(shutdown); err != nil {
		t.Fatalf("shutdown after EndStreams: %v after %s", err, time.Since(start))
	}
	cancel()
	<-sam.Done()
}

func TestDiscipleInitRunsSyncAndNotification(t *testing.T) {
	t.Setenv("SAUTH_DEBUG_USER_ID", "")
	t.Setenv("SAUTH_DEBUG_CLIENTS", "")
	t.Setenv("SAUTH_ENDPOINT", "http://127.0.0.1:1")
	t.Setenv("SAUTH_APP_SLUG", "gotest-disciple-"+uuid.NewString())
	db, err := sdb.Init(echo.New().Logger)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()
	key := sam.SyncLockKey(os.Getenv("SAUTH_APP_SLUG"))
	tryLock := func() bool {
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
	ctx, cancel := context.WithCancel(t.Context())
	if err := sam.Init(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for tryLock() {
		if time.Now().After(deadline) {
			t.Fatalf("the sync never took the leader lock")
		}
		time.Sleep(50 * time.Millisecond)
	}
	select {
	case <-notification.Done():
		t.Fatalf("notification did not start with sam.Init")
	default:
	}
	cancel()
	select {
	case <-sam.Done():
	case <-time.After(15 * time.Second):
		t.Fatalf("Done not closed after cancel")
	}
	select {
	case <-notification.Done():
	default:
		t.Fatalf("sam.Done closed before notification ended")
	}
	if !tryLock() {
		t.Fatalf("the sync kept the leader lock after the end")
	}
}
