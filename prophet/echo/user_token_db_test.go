//go:build dbtest

package echo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	pecho "github.com/nimling/samna-auth-middleware/v2/prophet/echo"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
)

func getTokenPayloads(t *testing.T, rec *httptest.ResponseRecorder, id uuid.UUID) []map[string]any {
	t.Helper()
	var page struct {
		Data []pecho.SyncPayload `json:"data"`
	}
	var snapshot struct {
		Data struct {
			Records []pecho.SyncPayload `json:"records"`
		} `json:"data"`
	}
	payloads := []pecho.SyncPayload{}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err == nil {
		payloads = page.Data
	} else {
		if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
			t.Fatalf("decode %s: %v", rec.Body.String(), err)
		}
		payloads = snapshot.Data.Records
	}
	rows := make([]map[string]any, 0, 1)
	for _, p := range payloads {
		if p.EventType != "claimius.user_identity" {
			continue
		}
		row := map[string]any{}
		_ = json.Unmarshal(p.Payload, &row)
		if row["id"] == id.String() {
			rows = append(rows, row)
		}
	}
	return rows
}

func getValidateStatus(appID uuid.UUID, externalID string) (int, string) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"external_id":"`+externalID+`"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := echo.New().NewContext(req, rec)
	ctx.Set("app_id", appID)
	ctx.Set("request_id", uuid.New())
	_ = pecho.PostUserTokenValidate(ctx)
	return rec.Code, rec.Body.String()
}

func TestUserTokenFollowsItsOrganization(t *testing.T) {
	appID, claimID := getGrantFixture(t)
	ctx := context.Background()
	var orgID uuid.UUID
	if err := psdb.Instance().NewRaw(`SELECT sa_owner_id FROM claimius.claim WHERE id = ?`, claimID).Scan(ctx, &orgID); err != nil {
		t.Fatalf("read org: %v", err)
	}
	linked := postPeerApp(t, grantUser)
	unlinked := postPeerApp(t, grantUser)
	if _, err := psdb.Instance().ExecContext(ctx, `SELECT claimius.upsert_app_object(?, 'claimius.organization', ?, ?)`, linked.ID, orgID, grantUser); err != nil {
		t.Fatalf("link org: %v", err)
	}
	for _, app := range []uuid.UUID{linked.ID, unlinked.ID} {
		if _, err := psdb.Instance().ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Ada')`, grantUser, app); err != nil {
			t.Fatalf("app user: %v", err)
		}
	}
	homeHead, linkedHead := getAppHead(t, appID), getAppHead(t, linked.ID)

	tokenID := uuid.New()
	external := "ext-" + uuid.NewString()
	if _, err := psdb.Instance().ExecContext(ctx, `
		INSERT INTO claimius.user_identity (id, app_id, user_id, external_id, type, data, sa_owner_id)
		VALUES (?, ?, ?, ?, 'external', '{"sub":"secret"}', ?)
	`, tokenID, appID, grantUser, external, orgID); err != nil {
		t.Fatalf("token: %v", err)
	}

	for _, c := range []struct {
		app  uuid.UUID
		head client.SyncCursor
	}{{appID, homeHead}, {linked.ID, linkedHead}} {
		rows := getTokenPayloads(t, getSyncPage(c.app, c.head, ""), tokenID)
		if len(rows) != 1 || rows[0]["external_id"] != external {
			t.Fatalf("a token must reach every app its organization is linked to, app %s got %v", c.app, rows)
		}
		if _, ok := rows[0]["data"]; ok {
			t.Fatalf("a token leaves the prophet without its data, got %v", rows[0])
		}
	}
	if rows := getTokenPayloads(t, getSyncPage(unlinked.ID, client.SyncCursor{}, ""), tokenID); len(rows) != 0 {
		t.Fatalf("a token must not reach an app its organization is not linked to, got %v", rows)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/?consumer="+uuid.NewString(), nil)
	rec := httptest.NewRecorder()
	sctx := e.NewContext(req, rec)
	sctx.Set("app_id", linked.ID)
	sctx.Set("request_id", uuid.New())
	_ = pecho.GetSyncSnapshot(sctx)
	rows := getTokenPayloads(t, rec, tokenID)
	if len(rows) != 1 {
		t.Fatalf("the snapshot of a linked app must carry the token, got %d %s", rec.Code, rec.Body.String()[:min(len(rec.Body.String()), 300)])
	}
	if _, ok := rows[0]["data"]; ok {
		t.Fatalf("the snapshot leaves the token's data out, got %v", rows[0])
	}

	if code, body := getValidateStatus(linked.ID, external); code != http.StatusOK || !strings.Contains(body, grantUser.String()) {
		t.Fatalf("an app linked to the organization must resolve its token, got %d %s", code, body)
	}
	if code, _ := getValidateStatus(unlinked.ID, external); code != http.StatusUnauthorized {
		t.Fatalf("an app not linked to the organization must not resolve its token, got %d", code)
	}

	if _, err := psdb.Instance().ExecContext(ctx, `UPDATE claimius.app_object SET sa_deleted_at = now() WHERE app_id = ? AND object_type = 'claimius.organization' AND object_id = ?`, linked.ID, orgID); err != nil {
		t.Fatalf("unlink: %v", err)
	}
	var gone bool
	if err := psdb.Instance().NewRaw(`
		SELECT EXISTS (SELECT 1 FROM claimius.sync_event WHERE app_id = ? AND event_type = 'claimius.user_identity' AND operation = 'delete' AND payload ->> 'id' = ?)
	`, linked.ID, tokenID.String()).Scan(ctx, &gone); err != nil || !gone {
		t.Fatalf("unlinking the organization must delete its tokens from the app, got %v %v", gone, err)
	}
}

func TestAuditRowsReachTheirApp(t *testing.T) {
	appID, claimID := getGrantFixture(t)
	ctx := context.Background()
	var orgID uuid.UUID
	if err := psdb.Instance().NewRaw(`SELECT sa_owner_id FROM claimius.claim WHERE id = ?`, claimID).Scan(ctx, &orgID); err != nil {
		t.Fatalf("read org: %v", err)
	}
	head := getAppHead(t, appID)
	auditID := uuid.New()
	if _, err := psdb.Instance().ExecContext(ctx, `
		INSERT INTO claimius.audit (id, app_id, operation, type, object_type, object_id, sa_owner_id)
		VALUES (?, ?, 'create', 'claim', 'claimius.claim', ?, ?)
	`, auditID, appID, claimID, orgID); err != nil {
		t.Fatalf("audit: %v", err)
	}
	var envelope struct {
		Data []pecho.SyncPayload `json:"data"`
	}
	rec := getSyncPage(appID, head, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	found := false
	for _, p := range envelope.Data {
		if p.EventType == "claimius.audit" && strings.Contains(string(p.Payload), auditID.String()) {
			found = true
		}
	}
	if !found {
		t.Fatalf("an audit row must reach its app through the feed as claimius.audit, got %s", rec.Body.String())
	}
}

func getSnapshotRecorder(appID uuid.UUID) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/?consumer="+uuid.NewString(), nil)
	rec := httptest.NewRecorder()
	ctx := echo.New().NewContext(req, rec)
	ctx.Set("app_id", appID)
	ctx.Set("request_id", uuid.New())
	_ = pecho.GetSyncSnapshot(ctx)
	return rec
}

func TestUserIdentityClaimStaysWithItsApp(t *testing.T) {
	appID, claimID := getGrantFixture(t)
	ctx := context.Background()
	var orgID uuid.UUID
	if err := psdb.Instance().NewRaw(`SELECT sa_owner_id FROM claimius.claim WHERE id = ?`, claimID).Scan(ctx, &orgID); err != nil {
		t.Fatalf("read org: %v", err)
	}
	linked := postPeerApp(t, grantUser)
	if _, err := psdb.Instance().ExecContext(ctx, `SELECT claimius.upsert_app_object(?, 'claimius.organization', ?, ?)`, linked.ID, orgID, grantUser); err != nil {
		t.Fatalf("link org: %v", err)
	}
	homeHead, linkedHead := getAppHead(t, appID), getAppHead(t, linked.ID)

	identityID := uuid.New()
	if _, err := psdb.Instance().ExecContext(ctx, `
		INSERT INTO claimius.user_identity (id, app_id, user_id, external_id, type, claim_id, data, sa_owner_id)
		VALUES (?, ?, ?, ?, 'token', ?, '{"sub":"secret"}', ?)
	`, identityID, appID, grantUser, "ext-"+uuid.NewString(), claimID, orgID); err != nil {
		t.Fatalf("identity: %v", err)
	}

	for _, c := range []struct {
		name string
		rec  *httptest.ResponseRecorder
		want any
	}{
		{"own feed", getSyncPage(appID, homeHead, ""), claimID.String()},
		{"own snapshot", getSnapshotRecorder(appID), claimID.String()},
		{"linked feed", getSyncPage(linked.ID, linkedHead, ""), nil},
		{"linked snapshot", getSnapshotRecorder(linked.ID), nil},
	} {
		rows := getTokenPayloads(t, c.rec, identityID)
		if len(rows) != 1 {
			t.Fatalf("%s: the identity must reach the app once, got %d %s", c.name, c.rec.Code, c.rec.Body.String()[:min(len(c.rec.Body.String()), 300)])
		}
		got, ok := rows[0]["claim_id"]
		if !ok || got != c.want {
			t.Fatalf("%s: an identity carries its claim_id to its own app and a null claim_id to any other, got %v", c.name, rows[0])
		}
		if _, ok := rows[0]["data"]; ok {
			t.Fatalf("%s: an identity leaves the prophet without its data, got %v", c.name, rows[0])
		}
	}
}
