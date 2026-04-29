package echo_test

import (
	"net/http"
	"testing"

	"github.com/nimling/samna-auth-middleware/middleware"
	saecho "github.com/nimling/samna-auth-middleware/echo"
)

func TestGetMe(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/me", nil)
	ctx.Set(middleware.UserIDKey, debugUserID)
	if err := saecho.GetMe(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestPutMe(t *testing.T) {
	body := map[string]any{
		"first_name": "Test",
		"last_name":  "User",
	}
	ctx, rec := newCtx(t, http.MethodPut, "/api/me", body)
	ctx.Set(middleware.UserIDKey, debugUserID)
	if err := saecho.PutMe(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestGetMeClaims(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/me/claim", nil)
	ctx.Set(middleware.UserIDKey, debugUserID)
	if err := saecho.GetMeClaims(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestGetMeRelations(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/me/relation", nil)
	ctx.Set(middleware.UserIDKey, debugUserID)
	if err := saecho.GetMeRelations(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestGetMeOrganizations(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/me/organization", nil)
	ctx.Set(middleware.UserIDKey, debugUserID)
	if err := saecho.GetMeOrganizations(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestGetMeLocations(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/me/location", nil)
	ctx.Set(middleware.UserIDKey, debugUserID)
	if err := saecho.GetMeLocations(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestGetMeAssets(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/me/asset", nil)
	ctx.Set(middleware.UserIDKey, debugUserID)
	if err := saecho.GetMeAssets(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestGetMeAudit(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/me/audit", nil)
	ctx.Set(middleware.UserIDKey, debugUserID)
	if err := saecho.GetMeAudit(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}
