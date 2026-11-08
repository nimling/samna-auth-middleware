//go:build dbtest

package echo_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	pecho "github.com/nimling/samna-auth-middleware/v2/prophet/echo"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
)

func getClientGrantStatus(handler echo.HandlerFunc, method string, appID uuid.UUID, names []string, values []string, body string) (int, string) {
	e := echo.New()
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames(names...)
	ctx.SetParamValues(values...)
	ctx.Set("app_id", appID)
	ctx.Set("request_id", uuid.New())
	_ = handler(ctx)
	return rec.Code, rec.Body.String()
}

func TestClientClaimGrantsTheClientNamedInThePath(t *testing.T) {
	appID, claimID := getGrantFixture(t)
	ctx := context.Background()
	var id, clientID uuid.UUID
	if err := psdb.Instance().NewRaw(`
		INSERT INTO claimius.samna_client (name, app_id, secret_hash, sa_owner_id)
		VALUES ('Grant client', ?, 'hash', ?)
		RETURNING id, client_id
	`, appID, grantRoot).Scan(ctx, &id, &clientID); err != nil {
		t.Fatalf("seed samna_client: %v", err)
	}
	if _, err := psdb.Instance().ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Grant client')`, clientID, appID); err != nil {
		t.Fatalf("seed client user: %v", err)
	}

	var actor uuid.UUID
	if err := psdb.Instance().NewRaw(`SELECT id FROM claimius.user_claim WHERE claim_id = ? AND user_id = ? AND sa_deleted_at IS NULL`, claimID, grantUser).Scan(ctx, &actor); err != nil {
		t.Fatalf("read actor grant: %v", err)
	}
	body := `{"claim_id":"` + claimID.String() + `","owner_id":"` + grantRoot.String() + `","created_by":"` + actor.String() + `","reason":"client grant"}`
	if code, out := getClientGrantStatus(pecho.PostClientClaim, http.MethodPost, appID, []string{"id"}, []string{id.String()}, body); code != http.StatusCreated || !strings.Contains(out, clientID.String()) {
		t.Fatalf("client grant: want 201 holding client_id %s as the user, got %d %s", clientID, code, out)
	}
	if code, out := getClientGrantStatus(pecho.DeleteClientClaim, http.MethodDelete, appID, []string{"id", "claim_id"}, []string{id.String(), claimID.String()}, ""); code != http.StatusOK {
		t.Fatalf("client grant delete by claim id: want 200, got %d %s", code, out)
	}
}
