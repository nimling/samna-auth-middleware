package echo_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
	saecho "github.com/nimling/samna-auth-middleware/echo"
)

func TestGetOrganizations(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/organization", nil)
	if err := saecho.GetOrganizations(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
	orgs := decodeData[[]db.Organization](t, rec)
	if len(orgs) == 0 {
		t.Fatal("expected at least one organization")
	}
}

func TestGetOrganization(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/organization/"+rootOrgID.String(), nil)
	setParam(ctx, "id", rootOrgID.String())
	if err := saecho.GetOrganization(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
	org := decodeData[db.Organization](t, rec)
	if org.ID != rootOrgID {
		t.Fatalf("got id %s want %s", org.ID, rootOrgID)
	}
}

func TestGetOrganizationBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/organization/not-a-uuid", nil)
	setParam(ctx, "id", "not-a-uuid")
	if err := saecho.GetOrganization(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestGetOrganizationNotFound(t *testing.T) {
	missing := uuid.New()
	ctx, rec := newCtx(t, http.MethodGet, "/api/organization/"+missing.String(), nil)
	setParam(ctx, "id", missing.String())
	if err := saecho.GetOrganization(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusNotFound)
}

func TestGetOrganizationGraph(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/graph/owner", nil)
	if err := saecho.GetOrganizationGraph(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestGetOrganizationAudit(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/organization/"+rootOrgID.String()+"/audit", nil)
	setParam(ctx, "id", rootOrgID.String())
	if err := saecho.GetOrganizationAudit(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestGetOrganizationAuditBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/organization/x/audit", nil)
	setParam(ctx, "id", "x")
	if err := saecho.GetOrganizationAudit(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPostOrganization(t *testing.T) {
	body := map[string]any{
		"name":     "Echo Test Org " + uuid.New().String(),
		"type":     "company",
		"owner_id": rootOrgID.String(),
	}
	ctx, rec := newCtx(t, http.MethodPost, "/api/organization", body)
	if err := saecho.PostOrganization(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusCreated)
	org := decodeData[db.Organization](t, rec)
	if org.ID == (uuid.UUID{}) {
		t.Fatal("expected new org id, got zero")
	}
	if org.Name == "" {
		t.Fatal("expected name, got empty")
	}
}

func TestPostOrganizationMissingOwnerID(t *testing.T) {
	body := map[string]any{
		"name": "Echo Test Org Missing Owner",
		"type": "company",
	}
	ctx, rec := newCtx(t, http.MethodPost, "/api/organization", body)
	if err := saecho.PostOrganization(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPostOrganizationSystemTypeRejected(t *testing.T) {
	body := map[string]any{
		"name":     "System Type Org",
		"type":     "system",
		"owner_id": rootOrgID.String(),
	}
	ctx, rec := newCtx(t, http.MethodPost, "/api/organization", body)
	if err := saecho.PostOrganization(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPostOrganizationForbiddenOwner(t *testing.T) {
	body := map[string]any{
		"name":     "Forbidden Owner Org",
		"type":     "company",
		"owner_id": uuid.New().String(),
	}
	ctx, rec := newCtx(t, http.MethodPost, "/api/organization", body)
	if err := saecho.PostOrganization(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusForbidden)
}

func TestPutOrganization(t *testing.T) {
	created := createTestOrg(t, "Echo PutOrg Source")
	defer deleteTestOrg(t, created.ID)

	body := map[string]any{
		"description": "updated description",
	}
	ctx, rec := newCtx(t, http.MethodPut, "/api/organization/"+created.ID.String(), body)
	setParam(ctx, "id", created.ID.String())
	if err := saecho.PutOrganization(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestPutOrganizationBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodPut, "/api/organization/x", map[string]any{})
	setParam(ctx, "id", "x")
	if err := saecho.PutOrganization(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPutOrganizationNotFound(t *testing.T) {
	missing := uuid.New()
	ctx, rec := newCtx(t, http.MethodPut, "/api/organization/"+missing.String(), map[string]any{})
	setParam(ctx, "id", missing.String())
	if err := saecho.PutOrganization(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusNotFound)
}

func TestDeleteOrganization(t *testing.T) {
	created := createTestOrg(t, "Echo DeleteOrg Source")

	ctx, rec := newCtx(t, http.MethodDelete, "/api/organization/"+created.ID.String(), nil)
	setParam(ctx, "id", created.ID.String())
	if err := saecho.DeleteOrganization(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestDeleteOrganizationBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodDelete, "/api/organization/x", nil)
	setParam(ctx, "id", "x")
	if err := saecho.DeleteOrganization(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestDeleteOrganizationNotFound(t *testing.T) {
	missing := uuid.New()
	ctx, rec := newCtx(t, http.MethodDelete, "/api/organization/"+missing.String(), nil)
	setParam(ctx, "id", missing.String())
	if err := saecho.DeleteOrganization(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusNotFound)
}

func createTestOrg(t *testing.T, name string) db.Organization {
	t.Helper()
	body := map[string]any{
		"name":     name + " " + uuid.New().String(),
		"type":     "company",
		"owner_id": rootOrgID.String(),
	}
	ctx, rec := newCtx(t, http.MethodPost, "/api/organization", body)
	if err := saecho.PostOrganization(ctx); err != nil {
		t.Fatalf("create test org: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create test org: status %d body %s", rec.Code, rec.Body.String())
	}
	return decodeData[db.Organization](t, rec)
}

func deleteTestOrg(t *testing.T, id uuid.UUID) {
	t.Helper()
	ctx, _ := newCtx(t, http.MethodDelete, "/api/organization/"+id.String(), nil)
	setParam(ctx, "id", id.String())
	_ = saecho.DeleteOrganization(ctx)
}
