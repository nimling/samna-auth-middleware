package echo_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
	saecho "github.com/nimling/samna-auth-middleware/echo"
)

func TestGetAssets(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/asset", nil)
	if err := saecho.GetAssets(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestGetAssetBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/asset/x", nil)
	setParam(ctx, "id", "x")
	if err := saecho.GetAsset(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestGetAssetNotFound(t *testing.T) {
	missing := uuid.New()
	ctx, rec := newCtx(t, http.MethodGet, "/api/asset/"+missing.String(), nil)
	setParam(ctx, "id", missing.String())
	if err := saecho.GetAsset(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusNotFound)
}

func TestGetAssetDataBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/asset/x/data", nil)
	setParam(ctx, "id", "x")
	if err := saecho.GetAssetData(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestGetAssetDataNotFound(t *testing.T) {
	missing := uuid.New()
	ctx, rec := newCtx(t, http.MethodGet, "/api/asset/"+missing.String()+"/data", nil)
	setParam(ctx, "id", missing.String())
	if err := saecho.GetAssetData(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusNotFound)
}

func TestPostAsset(t *testing.T) {
	body := map[string]any{
		"name":      "Echo Test Asset " + uuid.New().String(),
		"mime_type": "image/png",
		"user_id":   debugUserID.String(),
		"status":    "pending",
		"index":     0,
	}
	ctx, rec := newCtx(t, http.MethodPost, "/api/asset", body)
	if err := saecho.PostAsset(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusCreated)
	asset := decodeData[db.Asset](t, rec)
	if asset.ID == (uuid.UUID{}) {
		t.Fatal("expected asset id")
	}
	deleteTestAsset(t, asset.ID)
}

func TestPutAssetBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodPut, "/api/asset/x", map[string]any{})
	setParam(ctx, "id", "x")
	if err := saecho.PutAsset(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestDeleteAssetBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodDelete, "/api/asset/x", nil)
	setParam(ctx, "id", "x")
	if err := saecho.DeleteAsset(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPostAssetDataBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodPost, "/api/asset/x", nil)
	setParam(ctx, "id", "x")
	if err := saecho.PostAssetData(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPostAssetDataNotFound(t *testing.T) {
	missing := uuid.New()
	ctx, rec := newCtx(t, http.MethodPost, "/api/asset/"+missing.String(), nil)
	setParam(ctx, "id", missing.String())
	if err := saecho.PostAssetData(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusNotFound)
}

func deleteTestAsset(t *testing.T, id uuid.UUID) {
	t.Helper()
	ctx, _ := newCtx(t, http.MethodDelete, "/api/asset/"+id.String(), nil)
	setParam(ctx, "id", id.String())
	_ = saecho.DeleteAsset(ctx)
}
