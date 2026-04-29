package echo_test

import (
	"net/http"
	"testing"

	saecho "github.com/nimling/samna-auth-middleware/echo"
)

func TestPostSyncUnauthorized(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodPost, "/sync", map[string]any{})
	if err := saecho.PostSync(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusUnauthorized)
}
