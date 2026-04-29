package echo_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	saecho "github.com/nimling/samna-auth-middleware/echo"
)

func TestGetSecrets(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/secret", nil)
	if err := saecho.GetSecrets(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestGetSecretBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/secret/x", nil)
	setParam(ctx, "id", "x")
	if err := saecho.GetSecret(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestGetSecretNotFound(t *testing.T) {
	missing := uuid.New()
	ctx, rec := newCtx(t, http.MethodGet, "/api/secret/"+missing.String(), nil)
	setParam(ctx, "id", missing.String())
	if err := saecho.GetSecret(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusNotFound)
}

func TestPostSecretMissingFields(t *testing.T) {
	body := map[string]any{}
	ctx, rec := newCtx(t, http.MethodPost, "/api/secret", body)
	if err := saecho.PostSecret(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusInternalServerError)
}

func TestPutSecretBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodPut, "/api/secret/x", map[string]any{})
	setParam(ctx, "id", "x")
	if err := saecho.PutSecret(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestDeleteSecretBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodDelete, "/api/secret/x", nil)
	setParam(ctx, "id", "x")
	if err := saecho.DeleteSecret(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}
