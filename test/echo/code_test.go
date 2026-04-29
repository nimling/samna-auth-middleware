package echo_test

import (
	"net/http"
	"testing"

	saecho "github.com/nimling/samna-auth-middleware/echo"
)

func TestGetCodeBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/code/x", nil)
	setParam(ctx, "id", "x")
	if err := saecho.GetCode(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPutCodeBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodPut, "/api/code/x", map[string]any{})
	setParam(ctx, "id", "x")
	if err := saecho.PutCode(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestDeleteCodeBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodDelete, "/api/code/x", nil)
	setParam(ctx, "id", "x")
	if err := saecho.DeleteCode(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}
