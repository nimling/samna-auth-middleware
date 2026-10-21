package echo_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	pecho "github.com/nimling/samna-auth-middleware/v2/prophet/echo"
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

func TestPostCodeRequiresTheHashNeverAValue(t *testing.T) {
	body := `{"claim_id":"20000000-0000-4000-8000-000000000002","user_id":"40000000-0000-4000-8000-000000000004","kind":"guest","value":"plain"}`
	if code := postJSON(t, body, pecho.PostCode); code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when the body carries no hash, because the prophet stores the SHA-256 of a code and never its value", code)
	}
}

func TestPostCodeRequiresTheActor(t *testing.T) {
	body := `{"claim_id":"20000000-0000-4000-8000-000000000002","kind":"invite","hash":"` + strings.Repeat("a", 64) + `"}`
	if code := postJSON(t, body, pecho.PostCode); code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when user_id is absent, because insert_code checks that the actor owns the claim", code)
	}
}

func TestPostCodeRedeemRequiresTheUser(t *testing.T) {
	body := `{"hash":"` + strings.Repeat("a", 64) + `"}`
	if code := postJSON(t, body, pecho.PostCodeRedeem); code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 when user_id is absent, because an invite is redeemed by a signed in user", code)
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
