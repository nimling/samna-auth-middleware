package echo_test

import (
	"net/http"
	"testing"

	saecho "github.com/nimling/samna-auth-middleware/echo"
)

func TestGetClientBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/client/x", nil)
	setParam(ctx, "id", "x")
	if err := saecho.GetClient(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPutClientBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodPut, "/api/client/x", map[string]any{})
	setParam(ctx, "id", "x")
	if err := saecho.PutClient(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestDeleteClientBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodDelete, "/api/client/x", nil)
	setParam(ctx, "id", "x")
	if err := saecho.DeleteClient(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPostClientRotateBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodPost, "/api/client/x/rotate", nil)
	setParam(ctx, "id", "x")
	if err := saecho.PostClientRotate(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPostClientClaimBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodPost, "/api/client/x/claim", map[string]any{})
	setParam(ctx, "id", "x")
	if err := saecho.PostClientClaim(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestDeleteClientClaimBadIDs(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodDelete, "/api/client/x/claim/y", nil)
	setParam(ctx, "id", "x")
	setParam(ctx, "claim_id", "y")
	if err := saecho.DeleteClientClaim(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}
