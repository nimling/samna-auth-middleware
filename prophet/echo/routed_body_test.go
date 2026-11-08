package echo_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	pecho "github.com/nimling/samna-auth-middleware/prophet/echo"
	"github.com/nimling/samna-auth-middleware/shared"
)

func TestPostClaimObjectBindsADiscipleBindingToItsOwnTable(t *testing.T) {
	objectID := uuid.New()
	createdBy := uuid.New()
	objectType := shared.ObjectType("public.checkin")
	routed := &shared.ClaimObject{
		ClaimID:    uuid.New(),
		ObjectID:   &objectID,
		ObjectType: &objectType,
		OwnerID:    uuid.New(),
		RootID:     uuid.New(),
		CreatedBy:  &createdBy,
		Inherits:   true,
	}
	body, err := routed.MarshalWrite()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	ctx := echo.New().NewContext(req, httptest.NewRecorder())
	bound := &pecho.ClaimObject{}
	if err := bound.Bind(ctx); err != nil {
		t.Fatalf("the prophet must bind the body a disciple sends for its own table, body %s: %v", body, err)
	}
	if err := bound.ValidatePost(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if bound.ObjectType == nil || string(*bound.ObjectType) != "public.checkin" {
		t.Fatalf("object_type: want public.checkin, got %v", bound.ObjectType)
	}
}

func TestPostOrganizationAndLocationRequireTheActor(t *testing.T) {
	createdBy := uuid.New()
	ownerID := uuid.New()
	root := &pecho.Organization{Name: "Root", CreatedBy: &createdBy}
	if err := root.ValidatePost(); err != nil {
		t.Fatalf("a root organization carries no owner_id and must validate: %v", err)
	}
	child := &pecho.Organization{Name: "Child", OwnerID: &ownerID}
	if err := child.ValidatePost(); err == nil {
		t.Fatal("an organization without created_by must be refused before the insert")
	}
	location := &pecho.Location{Name: "Floor", OwnerID: ownerID}
	if err := location.ValidatePost(); err == nil {
		t.Fatal("a location without created_by must be refused before the insert")
	}
	location.CreatedBy = &createdBy
	if err := location.ValidatePost(); err != nil {
		t.Fatalf("location: %v", err)
	}
}
