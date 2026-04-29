package echo_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	saecho "github.com/nimling/samna-auth-middleware/echo"
)

func TestGetUsers(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/user", nil)
	if err := saecho.GetUsers(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestGetUserBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/user/x", nil)
	setParam(ctx, "id", "x")
	if err := saecho.GetUser(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestGetUserOK(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/user/"+debugUserID.String(), nil)
	setParam(ctx, "id", debugUserID.String())
	if err := saecho.GetUser(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestGetUserAuditBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/user/x/audit", nil)
	setParam(ctx, "id", "x")
	if err := saecho.GetUserAudit(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestGetUserAuditOK(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/user/"+debugUserID.String()+"/audit", nil)
	setParam(ctx, "id", debugUserID.String())
	if err := saecho.GetUserAudit(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestGetUserClaimsBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/user/x/claim", nil)
	setParam(ctx, "id", "x")
	if err := saecho.GetUserClaims(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestGetUserClaimsOK(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/user/"+debugUserID.String()+"/claim", nil)
	setParam(ctx, "id", debugUserID.String())
	if err := saecho.GetUserClaims(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestGetUserRelationsBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/user/x/relation", nil)
	setParam(ctx, "id", "x")
	if err := saecho.GetUserRelations(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestGetUserRelationsOK(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/user/"+debugUserID.String()+"/relation", nil)
	setParam(ctx, "id", debugUserID.String())
	if err := saecho.GetUserRelations(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestPostUserBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodPost, "/api/user/x", map[string]any{})
	setParam(ctx, "id", "x")
	if err := saecho.PostUser(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPutUserBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodPut, "/api/user/x", map[string]any{})
	setParam(ctx, "id", "x")
	if err := saecho.PutUser(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPutUserOK(t *testing.T) {
	body := map[string]any{
		"first_name": "Test",
	}
	ctx, rec := newCtx(t, http.MethodPut, "/api/user/"+debugUserID.String(), body)
	setParam(ctx, "id", debugUserID.String())
	if err := saecho.PutUser(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestDeleteUserBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodDelete, "/api/user/x", nil)
	setParam(ctx, "id", "x")
	if err := saecho.DeleteUser(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestGetUserAssetsBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/user/x/asset", nil)
	setParam(ctx, "id", "x")
	if err := saecho.GetUserAssets(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestGetUserAssetsOK(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/user/"+debugUserID.String()+"/asset", nil)
	setParam(ctx, "id", debugUserID.String())
	if err := saecho.GetUserAssets(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

var _ = uuid.New
