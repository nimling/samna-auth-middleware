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
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func TestPostUserTokenValidateReturnsTheMappingOwner(t *testing.T) {
	appID, _ := getGrantFixture(t)
	ctx := context.Background()
	if _, err := psdb.Instance().ExecContext(ctx, `
		INSERT INTO claimius.user_identity (app_id, user_id, id_claim, external_id, sa_owner_id)
		SELECT ?, ?, 'sub', 'ext-sub-owner', ?
		WHERE NOT EXISTS (SELECT 1 FROM claimius.user_identity WHERE external_id = 'ext-sub-owner')
	`, appID, grantUser, grantRoot); err != nil {
		t.Fatalf("seed user_identity: %v", err)
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

func TestGetUserAuditAnswersTheUsersAudit(t *testing.T) {
	appID, _ := getGrantFixture(t)

	e := echo.New()
	rec := httptest.NewRecorder()
	ectx := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec)
	ectx.SetParamNames("id")
	ectx.SetParamValues(grantUser.String())
	ectx.Set("app_id", appID)
	ectx.Set("request_id", uuid.New())
	_ = pecho.GetUserAudit(ectx)

	if rec.Code != http.StatusOK {
		t.Fatalf("user audit: want 200, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestGetUserOrganizationsAndLocationsAnswerTheUsersTrees(t *testing.T) {
	appID, _ := getGrantFixture(t)

	for name, handler := range map[string]echo.HandlerFunc{"organizations": pecho.GetUserOrganizations, "locations": pecho.GetUserLocations} {
		e := echo.New()
		rec := httptest.NewRecorder()
		ectx := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), rec)
		ectx.SetParamNames("id")
		ectx.SetParamValues(grantUser.String())
		ectx.Set("app_id", appID)
		ectx.Set("request_id", uuid.New())
		_ = handler(ectx)

		if rec.Code != http.StatusOK {
			t.Fatalf("user %s: want 200, got %d %s", name, rec.Code, rec.Body.String())
		}
	}
}

func TestDeleteUserAnswersTheDeletedRowARoutedDeleteReads(t *testing.T) {
	appID, _ := getGrantFixture(t)
	ctx := context.Background()
	userID := uuid.New()
	if _, err := psdb.Instance().ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Leaving')`, userID, appID); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	e := echo.New()
	rec := httptest.NewRecorder()
	ectx := e.NewContext(httptest.NewRequest(http.MethodDelete, "/", nil), rec)
	ectx.SetParamNames("id")
	ectx.SetParamValues(userID.String())
	ectx.Set("app_id", appID)
	ectx.Set("request_id", uuid.New())
	_ = pecho.DeleteUser(ectx)

	if rec.Code != http.StatusOK {
		t.Fatalf("delete user: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	var deleted shared.SamnaUser
	if err := deleted.UnmarshalWrite(body.Data); err != nil {
		t.Fatalf("a routed delete must read the answer as the user, body %s: %v", rec.Body.String(), err)
	}
	if deleted.UserID != userID || deleted.DeletedAt == nil {
		t.Fatalf("delete user: want user %s soft deleted, got user %s deleted_at %v", userID, deleted.UserID, deleted.DeletedAt)
	}
}
