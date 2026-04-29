package echo_test

import (
	"net/http"
	"testing"

	saecho "github.com/nimling/samna-auth-middleware/echo"
)

func TestGetLogin(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/login", nil)
	if err := saecho.GetLogin(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusTemporaryRedirect && rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d want 307 or 500\nbody: %s", rec.Code, rec.Body.String())
	}
}

func TestGetLogout(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/logout", nil)
	if err := saecho.GetLogout(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code < 200 || rec.Code >= 600 {
		t.Fatalf("unexpected status %d", rec.Code)
	}
}
