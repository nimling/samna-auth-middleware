package smw

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func TestForwardUserExternalRefusesASecondForwardedIdentity(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(userTokenHeader, "token-of-one-user")
	req.Header.Set(userExternalHeader, "external-id-of-another")
	ctx := echo.New().NewContext(req, httptest.NewRecorder())
	ctx.Set(UserIDKey, uuid.New())

	if err := forwardUserExternal(ctx); !errors.Is(err, ErrorForbidden) {
		t.Fatalf("a request forwarding both a user token and an external id: want ErrorForbidden, got %v", err)
	}
}
