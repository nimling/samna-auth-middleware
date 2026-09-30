package smw_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/nimtel"
	"github.com/nimling/samna-auth-middleware/v2/smw"
	"go.opentelemetry.io/otel/baggage"
)

func TestStampOrganization_CarriesRootAndOwner(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := e.NewContext(req, httptest.NewRecorder())
	root := uuid.New()
	owner := uuid.New()

	smw.StampOrganization(ctx, root, owner)

	bag := baggage.FromContext(ctx.Request().Context())
	if got := bag.Member(nimtel.RootKey).Value(); got != root.String() {
		t.Fatalf("root: want %s, got %q", root, got)
	}
	if got := bag.Member(nimtel.OwnerKey).Value(); got != owner.String() {
		t.Fatalf("owner: want %s, got %q", owner, got)
	}
}

func TestTelemetryMiddleware_PassesThrough(t *testing.T) {
	t.Setenv("SAUTH_APP_SLUG", "unit")
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ok := func(ctx echo.Context) error { return ctx.NoContent(http.StatusOK) }

	if err := smw.TelemetryMiddleware()(ok)(ctx); err != nil {
		t.Fatalf("middleware: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}
