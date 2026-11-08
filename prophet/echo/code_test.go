package echo_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	pecho "github.com/nimling/samna-auth-middleware/v2/prophet/echo"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

func postJSON(t *testing.T, body string, handler echo.HandlerFunc) int {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.Set("app_id", uuid.New())
	ctx.Set("request_id", uuid.New())
	if err := handler(ctx); err != nil {
		t.Fatalf("handler returned %v", err)
	}
	return rec.Code
}

func TestPostCodeRequiresCreatedBy(t *testing.T) {
	body := `{"owner_id":"10000000-0000-4000-8000-000000000001","claim_id":"20000000-0000-4000-8000-000000000002"}`
	if code := postJSON(t, body, pecho.PostCode); code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when created_by is absent, because sa_created_by is not null and the redeem hands it to insert_user_claim", code)
	}
}

func TestPostCodeRequiresOwnerID(t *testing.T) {
	body := `{"created_by":"30000000-0000-4000-8000-000000000003"}`
	if code := postJSON(t, body, pecho.PostCode); code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when owner_id is absent", code)
	}
}

func TestPostCodeRefusesUserAndClaimTogether(t *testing.T) {
	body := `{"owner_id":"10000000-0000-4000-8000-000000000001","created_by":"30000000-0000-4000-8000-000000000003","user_id":"40000000-0000-4000-8000-000000000004","claim_id":"20000000-0000-4000-8000-000000000002"}`
	if code := postJSON(t, body, pecho.PostCode); code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when a code carries both user_id and claim_id", code)
	}
}

func TestPostGuestTokenRequiresUserID(t *testing.T) {
	body := `{"claim_id":"20000000-0000-4000-8000-000000000002"}`
	if code := postJSON(t, body, pecho.PostGuestToken); code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when user_id is absent, because the claim access check reads it", code)
	}
}

func TestPostGuestTokenRequiresClaimID(t *testing.T) {
	body := `{"user_id":"40000000-0000-4000-8000-000000000004"}`
	if code := postJSON(t, body, pecho.PostGuestToken); code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when claim_id is absent", code)
	}
}

func TestCodeLeavesItsValueToTheDatabaseDefault(t *testing.T) {
	field, ok := bun.NewDB(&sql.DB{}, pgdialect.New()).Table(reflect.TypeOf(pecho.Code{})).FieldMap["code"]
	if !ok || !field.NullZero {
		t.Errorf("Code.Value must be nullzero, so an insert without a value takes gen_code_value() instead of an empty code that collides on uq_code_value")
	}
}
