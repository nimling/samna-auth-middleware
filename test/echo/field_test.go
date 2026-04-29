package echo_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	saecho "github.com/nimling/samna-auth-middleware/echo"
)

func TestGetObjectFields(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/field", nil)
	if err := saecho.GetObjectFields(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestGetObjectFieldBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/field/x", nil)
	setParam(ctx, "id", "x")
	if err := saecho.GetObjectField(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestGetObjectFieldNotFound(t *testing.T) {
	missing := uuid.New()
	ctx, rec := newCtx(t, http.MethodGet, "/api/field/"+missing.String(), nil)
	setParam(ctx, "id", missing.String())
	if err := saecho.GetObjectField(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusNotFound)
}

func TestPutObjectFieldBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodPut, "/api/field/x", map[string]any{})
	setParam(ctx, "id", "x")
	if err := saecho.PutObjectField(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestDeleteObjectFieldBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodDelete, "/api/field/x", nil)
	setParam(ctx, "id", "x")
	if err := saecho.DeleteObjectField(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestGetUserFieldsBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/field/x/value", nil)
	setParam(ctx, "id", "x")
	if err := saecho.GetUserFields(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPostUserFieldBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodPost, "/api/field/x/value", map[string]any{})
	setParam(ctx, "id", "x")
	if err := saecho.PostUserField(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPutUserFieldBadIDs(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodPut, "/api/field/x/value/y", map[string]any{})
	setParam(ctx, "id", "x")
	setParam(ctx, "value_id", "y")
	if err := saecho.PutUserField(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestDeleteUserFieldBadIDs(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodDelete, "/api/field/x/value/y", nil)
	setParam(ctx, "id", "x")
	setParam(ctx, "value_id", "y")
	if err := saecho.DeleteUserField(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}
