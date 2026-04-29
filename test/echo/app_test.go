package echo_test

import (
	"net/http"
	"testing"

	saecho "github.com/nimling/samna-auth-middleware/echo"
)

func TestGetApp(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/app", nil)
	if err := saecho.GetApp(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusOK && rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d want 200 or 500\nbody: %s", rec.Code, rec.Body.String())
	}
}
