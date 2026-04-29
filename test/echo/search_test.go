package echo_test

import (
	"net/http"
	"testing"

	saecho "github.com/nimling/samna-auth-middleware/echo"
)

func TestGetSearchMissingQuery(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/search", nil)
	if err := saecho.GetSearch(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestGetSearchOK(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/search?q=debug", nil)
	if err := saecho.GetSearch(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}
