package echo_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
	saecho "github.com/nimling/samna-auth-middleware/echo"
)

func TestGetLocations(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/location", nil)
	if err := saecho.GetLocations(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestGetLocationBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/location/x", nil)
	setParam(ctx, "id", "x")
	if err := saecho.GetLocation(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestGetLocationNotFound(t *testing.T) {
	missing := uuid.New()
	ctx, rec := newCtx(t, http.MethodGet, "/api/location/"+missing.String(), nil)
	setParam(ctx, "id", missing.String())
	if err := saecho.GetLocation(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusNotFound)
}

func TestGetLocationAuditBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/location/x/audit", nil)
	setParam(ctx, "id", "x")
	if err := saecho.GetLocationAudit(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestGetLocationAuditOK(t *testing.T) {
	loc := createTestLocation(t, "Echo Location for Audit")
	defer deleteTestLocation(t, loc.ID)

	ctx, rec := newCtx(t, http.MethodGet, "/api/location/"+loc.ID.String()+"/audit", nil)
	setParam(ctx, "id", loc.ID.String())
	if err := saecho.GetLocationAudit(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestPostLocationOK(t *testing.T) {
	body := map[string]any{
		"name":     "Echo Test Location",
		"owner_id": rootOrgID.String(),
	}
	ctx, rec := newCtx(t, http.MethodPost, "/api/location", body)
	if err := saecho.PostLocation(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusCreated)
	loc := decodeData[db.Location](t, rec)
	if loc.ID == (uuid.UUID{}) {
		t.Fatal("expected location id, got zero")
	}
	deleteTestLocation(t, loc.ID)
}

func TestPostLocationForbidden(t *testing.T) {
	body := map[string]any{
		"name":     "Echo Forbidden Location",
		"owner_id": uuid.New().String(),
	}
	ctx, rec := newCtx(t, http.MethodPost, "/api/location", body)
	if err := saecho.PostLocation(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusForbidden)
}

func TestPutLocation(t *testing.T) {
	loc := createTestLocation(t, "Echo Put Location")
	defer deleteTestLocation(t, loc.ID)

	body := map[string]any{
		"description": "updated",
	}
	ctx, rec := newCtx(t, http.MethodPut, "/api/location/"+loc.ID.String(), body)
	setParam(ctx, "id", loc.ID.String())
	if err := saecho.PutLocation(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestPutLocationBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodPut, "/api/location/x", map[string]any{})
	setParam(ctx, "id", "x")
	if err := saecho.PutLocation(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPutLocationForbidden(t *testing.T) {
	missing := uuid.New()
	ctx, rec := newCtx(t, http.MethodPut, "/api/location/"+missing.String(), map[string]any{})
	setParam(ctx, "id", missing.String())
	if err := saecho.PutLocation(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusForbidden)
}

func TestDeleteLocation(t *testing.T) {
	loc := createTestLocation(t, "Echo Delete Location")
	ctx, rec := newCtx(t, http.MethodDelete, "/api/location/"+loc.ID.String(), nil)
	setParam(ctx, "id", loc.ID.String())
	if err := saecho.DeleteLocation(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestDeleteLocationBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodDelete, "/api/location/x", nil)
	setParam(ctx, "id", "x")
	if err := saecho.DeleteLocation(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func createTestLocation(t *testing.T, name string) db.Location {
	t.Helper()
	body := map[string]any{
		"name":     name + " " + uuid.New().String(),
		"owner_id": rootOrgID.String(),
	}
	ctx, rec := newCtx(t, http.MethodPost, "/api/location", body)
	if err := saecho.PostLocation(ctx); err != nil {
		t.Fatalf("create test location: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create test location: status %d body %s", rec.Code, rec.Body.String())
	}
	return decodeData[db.Location](t, rec)
}

func deleteTestLocation(t *testing.T, id uuid.UUID) {
	t.Helper()
	ctx, _ := newCtx(t, http.MethodDelete, "/api/location/"+id.String(), nil)
	setParam(ctx, "id", id.String())
	_ = saecho.DeleteLocation(ctx)
}
