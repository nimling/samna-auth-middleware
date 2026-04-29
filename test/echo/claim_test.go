package echo_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
	saecho "github.com/nimling/samna-auth-middleware/echo"
)

func TestGetClaims(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/claim", nil)
	if err := saecho.GetClaims(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestGetClaimGraph(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/graph/claim", nil)
	if err := saecho.GetClaimGraph(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestGetClaimBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/claim/x", nil)
	setParam(ctx, "id", "x")
	if err := saecho.GetClaim(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestGetClaimNotFound(t *testing.T) {
	missing := uuid.New()
	ctx, rec := newCtx(t, http.MethodGet, "/api/claim/"+missing.String(), nil)
	setParam(ctx, "id", missing.String())
	if err := saecho.GetClaim(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusNotFound)
}

func TestGetClaimAuditBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodGet, "/api/claim/x/audit", nil)
	setParam(ctx, "id", "x")
	if err := saecho.GetClaimAudit(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestGetClaimAuditOK(t *testing.T) {
	c := createTestClaim(t, "Echo Claim Audit")
	defer deleteTestClaim(t, c.ID)

	ctx, rec := newCtx(t, http.MethodGet, "/api/claim/"+c.ID.String()+"/audit", nil)
	setParam(ctx, "id", c.ID.String())
	if err := saecho.GetClaimAudit(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusOK)
}

func TestPostClaimMissingOwner(t *testing.T) {
	body := map[string]any{
		"name":         "Echo Test Claim",
		"access_level": int(db.ACCESS_LEVEL_READ),
	}
	ctx, rec := newCtx(t, http.MethodPost, "/api/claim", body)
	if err := saecho.PostClaim(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPostClaimSystemTypeRejected(t *testing.T) {
	body := map[string]any{
		"name":         "Bad System Claim",
		"type":         string(db.CLAIM_TYPE_SYSTEM),
		"access_level": int(db.ACCESS_LEVEL_READ),
		"owner_id":     rootOrgID.String(),
	}
	ctx, rec := newCtx(t, http.MethodPost, "/api/claim", body)
	if err := saecho.PostClaim(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPostClaimSystemLevelRejected(t *testing.T) {
	body := map[string]any{
		"name":         "Bad System Level Claim",
		"access_level": int(db.ACCESS_LEVEL_SYSTEM),
		"owner_id":     rootOrgID.String(),
	}
	ctx, rec := newCtx(t, http.MethodPost, "/api/claim", body)
	if err := saecho.PostClaim(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPostClaimOK(t *testing.T) {
	body := map[string]any{
		"name":         "Echo Test Claim " + uuid.New().String(),
		"description":  "echo test",
		"access_level": int(db.ACCESS_LEVEL_READ),
		"owner_id":     rootOrgID.String(),
		"type":         string(db.CLAIM_TYPE_USER),
	}
	ctx, rec := newCtx(t, http.MethodPost, "/api/claim", body)
	if err := saecho.PostClaim(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusCreated)
	c := decodeData[db.Claim](t, rec)
	if c.ID == (uuid.UUID{}) {
		t.Fatal("expected claim id")
	}
	deleteTestClaim(t, c.ID)
}

func TestPutClaimBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodPut, "/api/claim/x", map[string]any{"access_level": int(db.ACCESS_LEVEL_READ)})
	setParam(ctx, "id", "x")
	if err := saecho.PutClaim(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPutClaimSystemTypeRejected(t *testing.T) {
	c := createTestClaim(t, "Echo PutClaim System Reject")
	defer deleteTestClaim(t, c.ID)

	body := map[string]any{
		"type":         string(db.CLAIM_TYPE_SYSTEM),
		"access_level": int(db.ACCESS_LEVEL_READ),
	}
	ctx, rec := newCtx(t, http.MethodPut, "/api/claim/"+c.ID.String(), body)
	setParam(ctx, "id", c.ID.String())
	if err := saecho.PutClaim(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPutClaimForbidden(t *testing.T) {
	missing := uuid.New()
	body := map[string]any{
		"access_level": int(db.ACCESS_LEVEL_READ),
	}
	ctx, rec := newCtx(t, http.MethodPut, "/api/claim/"+missing.String(), body)
	setParam(ctx, "id", missing.String())
	if err := saecho.PutClaim(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusForbidden)
}

func TestDeleteClaimBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodDelete, "/api/claim/x", nil)
	setParam(ctx, "id", "x")
	if err := saecho.DeleteClaim(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestDeleteClaimForbidden(t *testing.T) {
	missing := uuid.New()
	ctx, rec := newCtx(t, http.MethodDelete, "/api/claim/"+missing.String(), nil)
	setParam(ctx, "id", missing.String())
	if err := saecho.DeleteClaim(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusForbidden)
}

func TestPostClaimObjectBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodPost, "/api/claim/x/object", map[string]any{})
	setParam(ctx, "id", "x")
	if err := saecho.PostClaimObject(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPostClaimObjectForbidden(t *testing.T) {
	missing := uuid.New()
	body := map[string]any{
		"object_id":   uuid.New().String(),
		"object_type": db.OBJECT_TYPE_ORGANIZATION,
	}
	ctx, rec := newCtx(t, http.MethodPost, "/api/claim/"+missing.String()+"/object", body)
	setParam(ctx, "id", missing.String())
	if err := saecho.PostClaimObject(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusForbidden)
}

func TestDeleteClaimObjectBadIDs(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodDelete, "/api/claim/x/object/y", nil)
	setParam(ctx, "id", "x")
	setParam(ctx, "object_id", "y")
	if err := saecho.DeleteClaimObject(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestPostClaimUserBadID(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodPost, "/api/claim/x/user", map[string]any{})
	setParam(ctx, "id", "x")
	if err := saecho.PostClaimUser(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func TestDeleteClaimUserBadIDs(t *testing.T) {
	ctx, rec := newCtx(t, http.MethodDelete, "/api/claim/x/user/y", nil)
	setParam(ctx, "id", "x")
	setParam(ctx, "user_id", "y")
	if err := saecho.DeleteClaimUser(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	assertStatus(t, rec, http.StatusBadRequest)
}

func createTestClaim(t *testing.T, name string) db.Claim {
	t.Helper()
	body := map[string]any{
		"name":         name + " " + uuid.New().String(),
		"description":  "echo test",
		"access_level": int(db.ACCESS_LEVEL_READ),
		"owner_id":     rootOrgID.String(),
		"type":         string(db.CLAIM_TYPE_USER),
	}
	ctx, rec := newCtx(t, http.MethodPost, "/api/claim", body)
	if err := saecho.PostClaim(ctx); err != nil {
		t.Fatalf("create test claim: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create test claim: status %d body %s", rec.Code, rec.Body.String())
	}
	return decodeData[db.Claim](t, rec)
}

func deleteTestClaim(t *testing.T, id uuid.UUID) {
	t.Helper()
	ctx, _ := newCtx(t, http.MethodDelete, "/api/claim/"+id.String(), nil)
	setParam(ctx, "id", id.String())
	_ = saecho.DeleteClaim(ctx)
}
