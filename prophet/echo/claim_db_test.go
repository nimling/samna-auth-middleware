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

var (
	grantUser   = uuid.MustParse("a0000000-0000-4000-8000-0000000000c1")
	grantRoot   = uuid.MustParse("10000000-0000-4000-8000-0000000000c1")
	grantObject = uuid.MustParse("10000000-0000-4000-8000-0000000000c2")
	otherApp    = uuid.MustParse("50000000-0000-4000-8000-0000000000c9")
)

const grantSeed = `
DO $$
DECLARE
    v_init   JSONB;
    v_app    UUID;
    v_user   UUID := 'a0000000-0000-4000-8000-0000000000c1';
    v_root   UUID := '10000000-0000-4000-8000-0000000000c1';
    v_object UUID := '10000000-0000-4000-8000-0000000000c2';
    v_claim  UUID;
BEGIN
    IF EXISTS (SELECT 1 FROM claimius.claim WHERE name = 'Grant writer') THEN
        RETURN;
    END IF;
    v_init := claimius.upsert_system_app(
        p_system_app_slug         => 'gotest',
        p_system_app_name         => 'Go test',
        p_system_app_private_key  => 'key',
        p_system_app_private_seed => 'seed',
        p_default_claim           => '{"name": "Member", "description": "Default member claim"}'::JSONB
    );
    v_app := (v_init -> 'samna_app' ->> 'id')::UUID;

    INSERT INTO claimius.samna_app (id, slug, name, private_key, private_seed, sa_owner_id, sa_created_by)
    SELECT '50000000-0000-4000-8000-0000000000c9', 'gotest_other', 'Other', 'key', 'seed', sa_owner_id, sa_created_by
    FROM claimius.samna_app WHERE id = v_app;

    PERFORM claimius.upsert_app_user(p_user_id => v_user, p_app_id => v_app, p_first_name => 'Ada');
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_root, 'Grant root', 'company', v_root, v_root, 0, v_user),
           (v_object, 'Grant object', 'company', v_root, v_root, 1, v_user);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', v_root);
    PERFORM claimius.upsert_app_object(v_app, 'claimius.organization', v_object);

    v_claim := (claimius.insert_claim(
        p_app_id => v_app, p_name => 'Grant writer', p_description => NULL, p_sa_access => 6,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_user, p_inherits => TRUE
    ) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(
        p_app_id => v_app, p_claim_id => v_claim, p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_user,
        p_object_id => v_object, p_object_type => 'claimius.organization', p_sa_access => 6, p_inherits => TRUE
    );
    PERFORM claimius.insert_user_claim(p_app_id => v_app, p_claim_id => v_claim, p_user_id => v_user, p_sa_owner_id => v_root, p_sa_created_by => v_user);
END $$;
`

func getGrantFixture(t *testing.T) (appID, claimID uuid.UUID) {
	t.Helper()
	db, err := psdb.Init(echo.New().Logger)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, grantSeed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.NewRaw(`SELECT app_id, id FROM claimius.claim WHERE name = 'Grant writer' AND sa_deleted_at IS NULL`).
		Scan(ctx, &appID, &claimID); err != nil {
		t.Fatalf("read claim: %v", err)
	}
	return appID, claimID
}

func getPatchStatus(handler echo.HandlerFunc, appID, claimID, id uuid.UUID, body string) (int, string) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("claim_id", "id")
	ctx.SetParamValues(claimID.String(), id.String())
	ctx.Set("app_id", appID)
	ctx.Set("request_id", uuid.New())
	_ = handler(ctx)
	return rec.Code, rec.Body.String()
}

func getGrantRows(t *testing.T, claimID uuid.UUID) (grantID, bindingID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	if err := psdb.Instance().NewRaw(`SELECT id FROM claimius.user_claim WHERE claim_id = ? AND user_id = ? AND sa_deleted_at IS NULL`, claimID, grantUser).Scan(ctx, &grantID); err != nil {
		t.Fatalf("grant row: %v", err)
	}
	if err := psdb.Instance().NewRaw(`SELECT id FROM claimius.claim_object WHERE claim_id = ? AND object_id = ? AND sa_deleted_at IS NULL`, claimID, grantObject).Scan(ctx, &bindingID); err != nil {
		t.Fatalf("binding row: %v", err)
	}
	return grantID, bindingID
}

func TestPatchGrant_AddressesTheGrantTheWayTheDiscipleSendsIt(t *testing.T) {
	appID, claimID := getGrantFixture(t)
	grantID, bindingID := getGrantRows(t, claimID)

	if code, body := getPatchStatus(pecho.PatchClaimUser, appID, claimID, grantID, `{"reason":"renewed"}`); code != http.StatusOK || !strings.Contains(body, "renewed") {
		t.Fatalf("user grant patch by claim and grant id: want 200 with the new reason, got %d %s", code, body)
	}
	if code, body := getPatchStatus(pecho.PatchClaimObject, appID, claimID, bindingID, `{"reason":"narrowed"}`); code != http.StatusOK || !strings.Contains(body, "narrowed") {
		t.Fatalf("object grant patch by claim and binding id: want 200 with the new reason, got %d %s", code, body)
	}
}

func TestPatchGrant_RefusesAnotherAppsGrant(t *testing.T) {
	_, claimID := getGrantFixture(t)
	grantID, bindingID := getGrantRows(t, claimID)

	if code, _ := getPatchStatus(pecho.PatchClaimUser, otherApp, claimID, grantID, `{"reason":"stolen"}`); code != http.StatusNotFound {
		t.Fatalf("user grant patch from another app: want 404, got %d", code)
	}
	if code, _ := getPatchStatus(pecho.PatchClaimObject, otherApp, claimID, bindingID, `{"reason":"stolen"}`); code != http.StatusNotFound {
		t.Fatalf("object grant patch from another app: want 404, got %d", code)
	}
}

func TestPutClaimKeepsEveryFieldSentWithTheMask(t *testing.T) {
	appID, claimID := getGrantFixture(t)
	ctx := context.Background()
	var original struct {
		Description *string `bun:"description"`
		Type        string  `bun:"type"`
		Access      int     `bun:"sa_access"`
	}
	if err := psdb.Instance().NewRaw(`SELECT description, type, sa_access FROM claimius.claim WHERE id = ?`, claimID).Scan(ctx, &original); err != nil {
		t.Fatalf("read original: %v", err)
	}
	name := "Grant writer " + uuid.NewString()
	code, body := getPatchStatus(pecho.PutClaim, appID, claimID, claimID, `{"access":15,"name":"`+name+`","description":"renamed with the mask","type":"user"}`)
	if code != http.StatusOK {
		t.Fatalf("put claim: want 200, got %d %s", code, body)
	}
	var stored struct {
		Name        string `bun:"name"`
		Description string `bun:"description"`
		Type        string `bun:"type"`
		Access      int    `bun:"sa_access"`
	}
	if err := psdb.Instance().NewRaw(`SELECT name, description, type, sa_access FROM claimius.claim WHERE id = ?`, claimID).Scan(ctx, &stored); err != nil {
		t.Fatalf("read claim: %v", err)
	}
	if stored.Name != name || stored.Description != "renamed with the mask" || stored.Type != "user" || stored.Access != 15 {
		t.Fatalf("put claim: want every field sent stored, got %+v", stored)
	}
	if _, err := psdb.Instance().NewRaw(`UPDATE claimius.claim SET name = 'Grant writer', description = ?, type = ?, sa_access = ? WHERE id = ?`, original.Description, original.Type, original.Access, claimID).Exec(ctx); err != nil {
		t.Fatalf("restore claim: %v", err)
	}
}
