package notification_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/notification"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func TestInitWithoutDatabaseIsRefused(t *testing.T) {
	if err := notification.Init(t.Context()); !errors.Is(err, shared.ErrNotConnected) {
		t.Fatalf("init = %v, want ErrNotConnected", err)
	}
}

func TestCallsBeforeInitAnswerNotStarted(t *testing.T) {
	notification.WriteUser(notification.Notification{Title: "early", Type: "info"}, uuid.New())
	notification.Write(notification.Notification{Title: "early", Type: "info"}, "claimius.organization", uuid.New())
	e := echo.New()
	ctx := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder())
	if err := notification.Stream(ctx, uuid.New()); !errors.Is(err, notification.ErrNotStarted) {
		t.Fatalf("stream = %v, want ErrNotStarted", err)
	}
	select {
	case <-notification.Done():
	default:
		t.Fatalf("Done before Init blocks")
	}
}
