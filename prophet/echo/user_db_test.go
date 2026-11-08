//go:build dbtest

package echo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	pecho "github.com/nimling/samna-auth-middleware/v2/prophet/echo"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
)

func TestPostUserTokenValidateReturnsTheMappingOwner(t *testing.T) {
	appID, _ := getGrantFixture(t)
	ctx := context.Background()
	if _, err := psdb.Instance().ExecContext(ctx, `
		INSERT INTO claimius.user_token (app_id, user_id, id_claim, external_id, sa_owner_id)
		SELECT ?, ?, 'sub', 'ext-sub-owner', ?
		WHERE NOT EXISTS (SELECT 1 FROM claimius.user_token WHERE external_id = 'ext-sub-owner')
	`, appID, grantUser, grantRoot); err != nil {
		t.Fatalf("seed user_token: %v", err)
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "ext-sub-owner"}).SignedString([]byte("host-system"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"token":"`+token+`"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ectx := e.NewContext(req, rec)
	ectx.Set("app_id", appID)
	ectx.Set("request_id", uuid.New())
	_ = pecho.PostUserTokenValidate(ectx)

	var body struct {
		Data struct {
			UserID  uuid.UUID `json:"user_id"`
			OwnerID uuid.UUID `json:"owner_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("validate: want 200 with a body, got %d %s", rec.Code, rec.Body.String())
	}
	if body.Data.UserID != grantUser || body.Data.OwnerID != grantRoot {
		t.Fatalf("validate: want user %s owned by %s, got user %s owner %s", grantUser, grantRoot, body.Data.UserID, body.Data.OwnerID)
	}
}
