//go:build dbtest

package echo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/smw"
)

func TestTheProphetHostResolvesAnAnonymousRequestPerApp(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	prophet := psdb.Instance()
	app := postPeerApp(t, grantUser)

	for _, a := range []uuid.UUID{app.ID} {
		if _, err := prophet.ExecContext(ctx, `
			INSERT INTO claimius.samna_user (user_id, app_id, status, type, first_name)
			VALUES (claimius.anonymous_user_id(), ?, 'active', 'anonymous', 'Public')
			ON CONFLICT (user_id, app_id) WHERE sa_deleted_at IS NULL DO NOTHING`, a); err != nil {
			t.Fatalf("the anonymous user of an app the test inserted directly, as upsert_app and the init backfill write it: %v", err)
		}
	}

	var readers uuid.UUID
	if err := prophet.NewRaw(`
		SELECT (claimius.insert_claim(p_app_id => ?, p_name => 'Public readers', p_description => NULL, p_sa_access => 4,
		        p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?) -> 'claim' ->> 'id')::UUID
	`, app.ID, app.Org, app.Org, grantUser).Scan(ctx, &readers); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := prophet.ExecContext(ctx, `
		SELECT claimius.insert_claim_object(p_app_id => ?, p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
		       p_sa_created_by => ?, p_object_id => ?, p_object_type => 'claimius.organization', p_inherits => TRUE)
	`, app.ID, readers, app.Org, app.Org, grantUser, app.Org); err != nil {
		t.Fatalf("bind: %v", err)
	}
	options := &shared.ClaimOptions{Samk: &shared.ClaimOptionsSamk{Header: true}}
	value, err := options.Seal()
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	raw, _ := json.Marshal(options)
	if _, err := prophet.ExecContext(ctx, `
		SELECT claimius.insert_user_claim(p_app_id => ?, p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?, p_options => ?::jsonb)
	`, app.ID, readers, shared.AnonymousUserID, app.Org, grantUser, string(raw)); err != nil {
		t.Fatalf("public grant: %v", err)
	}
	shared.DeletePublicClaimCache()

	t.Setenv("SAUTH_REDIRECT_URI", "http://localhost:4200/api/auth/callback")
	handler := smw.AnonymousMiddleware(func(ctx echo.Context) error {
		userID, _ := ctx.Get(smw.UserIDKey).(uuid.UUID)
		var access int
		if err := prophet.NewRaw(`SELECT sa_access FROM claimius.get_access(?, ?, ?, 'claimius.organization', 4)`, userID, app.ID, app.Org).Scan(ctx.Request().Context(), &access); err != nil {
			return ctx.String(http.StatusForbidden, "Forbidden")
		}
		return ctx.JSON(http.StatusOK, map[string]any{"user_id": userID, "access": access})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/board", nil)
	req.Header.Set("SAuth-App-Slug", app.Slug)
	req.Header.Set(smw.CodeHeader, value)
	rec := httptest.NewRecorder()
	if err := handler(echo.New().NewContext(req, rec)); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("the prophet host resolves the anonymous user of the app named by SAuth-App-Slug with its code, got %d %v %s", rec.Code, err, rec.Body.String())
	}
	var answer struct {
		UserID uuid.UUID `json:"user_id"`
		Access int       `json:"access"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil || answer.UserID != shared.AnonymousUserID || answer.Access != 4 {
		t.Fatalf("the handler sees the anonymous user with the grant behind the code, got %+v %v", answer, err)
	}
	if req.Header.Get(smw.CodeHeader) != "" {
		t.Fatal("the code is taken off the request before the handler")
	}

	plain := httptest.NewRequest(http.MethodGet, "/api/board", nil)
	plain.Header.Set("SAuth-App-Slug", app.Slug)
	rec = httptest.NewRecorder()
	if err := handler(echo.New().NewContext(plain, rec)); err != nil || rec.Code != http.StatusUnauthorized {
		t.Fatalf("without the code the grant does not count and the 403 is answered 401, got %d %v", rec.Code, err)
	}

	other := postPeerApp(t, grantUser)
	elsewhere := httptest.NewRequest(http.MethodGet, "/api/board", nil)
	elsewhere.Header.Set("SAuth-App-Slug", other.Slug)
	elsewhere.Header.Set(smw.CodeHeader, value)
	rec = httptest.NewRecorder()
	if err := handler(echo.New().NewContext(elsewhere, rec)); err != nil || rec.Code != http.StatusUnauthorized {
		t.Fatalf("another app holds no public grant and answers 401 at once, got %d %v", rec.Code, err)
	}
}
