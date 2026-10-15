//go:build dbtest

package echo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	pecho "github.com/nimling/samna-auth-middleware/v2/prophet/echo"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func TestPostOrganizationLinksARootTheWayTheDiscipleSendsIt(t *testing.T) {
	appID, claimID := getGrantFixture(t)
	ctx := context.Background()
	var actor uuid.UUID
	if err := psdb.Instance().NewRaw(`SELECT id FROM claimius.user_claim WHERE claim_id = ? AND user_id = ? AND sa_deleted_at IS NULL`, claimID, grantUser).
		Scan(ctx, &actor); err != nil {
		t.Fatalf("read grant: %v", err)
	}
	rootID := uuid.New()
	routed := &shared.Organization{
		ID:        rootID,
		Name:      "Routed root " + rootID.String(),
		Type:      shared.ORGANIZATION_TYPE_COMPANY,
		OwnerID:   rootID,
		RootID:    rootID,
		CreatedBy: actor,
	}
	body, err := routed.MarshalWrite()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ectx := echo.New().NewContext(req, rec)
	ectx.Set("app_id", appID)
	ectx.Set("request_id", uuid.New())
	_ = pecho.PostOrganization(ectx)

	var created struct {
		Data struct {
			ID      uuid.UUID `json:"id"`
			OwnerID uuid.UUID `json:"owner_id"`
			RootID  uuid.UUID `json:"root_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("post root: want 201 with a body, got %d %s", rec.Code, rec.Body.String())
	}
	if created.Data.ID != rootID || created.Data.OwnerID != rootID || created.Data.RootID != rootID {
		t.Fatalf("post root: want the root %s the disciple chose, got %+v", rootID, created.Data)
	}
	var linked bool
	if err := psdb.Instance().NewRaw(`SELECT claimius.app_has_object(?, 'claimius.organization', ?)`, appID, rootID).Scan(ctx, &linked); err != nil {
		t.Fatalf("read link: %v", err)
	}
	if !linked {
		t.Fatalf("post root: the root %s must be linked to app %s", rootID, appID)
	}
}

func TestPutOrganizationClearsAFieldSentAsNull(t *testing.T) {
	appID, claimID := getGrantFixture(t)
	ctx := context.Background()
	var rootID uuid.UUID
	if err := psdb.Instance().NewRaw(`SELECT claim.sa_owner_id FROM claimius.claim WHERE id = ?`, claimID).Scan(ctx, &rootID); err != nil {
		t.Fatalf("read root: %v", err)
	}
	external := "ext-" + uuid.NewString()
	stored := func() *string {
		var v *string
		if err := psdb.Instance().NewRaw(`SELECT external_id FROM claimius.organization WHERE id = ?`, rootID).Scan(ctx, &v); err != nil {
			t.Fatalf("read external id: %v", err)
		}
		return v
	}
	getPatchStatus(pecho.PutOrganization, appID, claimID, rootID, `{"external_id":"`+external+`"}`)
	if v := stored(); v == nil || *v != external {
		t.Fatalf("an external id sent must be stored, got %v", v)
	}
	getPatchStatus(pecho.PutOrganization, appID, claimID, rootID, `{"external_id":null}`)
	if v := stored(); v != nil {
		t.Fatalf("an external id sent as null must be cleared, got %q", *v)
	}
	if code, _ := getPatchStatus(pecho.PutOrganization, appID, claimID, rootID, `{}`); code != http.StatusBadRequest {
		t.Fatalf("an empty patch must be refused with 400, got %d", code)
	}
}
