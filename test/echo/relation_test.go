package echo_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/middleware"
	saecho "github.com/nimling/samna-auth-middleware/echo"
)

func TestGetRelations(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/relation", nil)
	ctx.Set(middleware.UserIDKey, debugUserID)
	if err := saecho.GetRelations(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestGetRelationsUnauthorized(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/relation", nil)
	if err := saecho.GetRelations(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusUnauthorized)
}

func TestGetRelationBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/relation/x", nil)
	setParam(ctx, "id", "x")
	ctx.Set(middleware.UserIDKey, debugUserID)
	if err := saecho.GetRelation(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestGetRelationUnauthorized(t *testing.T) {
	id := uuid.New().String()
	ctx, rec := newCtx(t, http.MethodGet, "/api/relation/"+id, nil)
	setParam(ctx, "id", id)
	if err := saecho.GetRelation(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusUnauthorized)
}

func TestPostRelationUnauthorized(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodPost, "/api/relation", map[string]any{})
	if err := saecho.PostRelation(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusUnauthorized)
}

func TestPutRelationBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodPut, "/api/relation/x", map[string]any{})
	setParam(ctx, "id", "x")
	if err := saecho.PutRelation(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestDeleteRelationBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodDelete, "/api/relation/x", nil)
	setParam(ctx, "id", "x")
	if err := saecho.DeleteRelation(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}
