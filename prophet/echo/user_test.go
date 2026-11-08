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

func TestPutUserRefusesTheUserImage(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(`{"user_image":"https://disciple.example.no/me.png"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.Set("app_id", uuid.New())
	ctx.Set("request_id", uuid.New())
	ctx.SetParamNames("id")
	ctx.SetParamValues(uuid.NewString())
	if err := pecho.PutUser(ctx); err != nil {
		t.Fatalf("handler returned %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400, because the image is the prophet's and only its share setting writes it", rec.Code)
	}
}
