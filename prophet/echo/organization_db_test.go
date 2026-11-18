//go:build dbtest

package echo_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	pecho "github.com/nimling/samna-auth-middleware/v2/prophet/echo"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func postRoutedOrganization(t *testing.T, appID uuid.UUID, routed *shared.Organization) *httptest.ResponseRecorder {
	t.Helper()
	body, err := routed.MarshalWrite()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ectx := echo.New().NewContext(req, rec)
	ectx.Set("app_id", appID)
	ectx.Set("request_id", uuid.New())
	_ = pecho.PostOrganization(ectx)
	return rec
}

func TestPostOrganizationRefusesARootFromADisciple(t *testing.T) {
	appID, claimID := getGrantFixture(t)
	ctx := context.Background()
	var actor uuid.UUID
	if err := psdb.Instance().NewRaw(`SELECT id FROM claimius.user_claim WHERE claim_id = ? AND user_id = ? AND sa_deleted_at IS NULL`, claimID, grantUser).
		Scan(ctx, &actor); err != nil {
		t.Fatalf("read grant: %v", err)
	}
	rootID := uuid.New()
	rec := postRoutedOrganization(t, appID, &shared.Organization{
		ID:        rootID,
		Name:      "Routed root " + rootID.String(),
		OwnerID:   rootID,
		RootID:    rootID,
		CreatedBy: actor,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("post root: want 403, got %d %s", rec.Code, rec.Body.String())
	}
	var stored int
	if err := psdb.Instance().NewRaw(`SELECT count(*) FROM claimius.organization WHERE id = ?`, rootID).Scan(ctx, &stored); err != nil {
		t.Fatalf("read root: %v", err)
	}
	if stored != 0 {
		t.Fatalf("post root: the root %s must not be stored", rootID)
	}
}

func TestPostOrganizationTakesAChildOnlyUnderAnOrganizationOfTheApp(t *testing.T) {
	appID, claimID := getGrantFixture(t)
	ctx := context.Background()
	var actor, parent uuid.UUID
	if err := psdb.Instance().NewRaw(`SELECT id FROM claimius.user_claim WHERE claim_id = ? AND user_id = ? AND sa_deleted_at IS NULL`, claimID, grantUser).
		Scan(ctx, &actor); err != nil {
		t.Fatalf("read grant: %v", err)
	}
	if err := psdb.Instance().NewRaw(`SELECT claim.sa_owner_id FROM claimius.claim WHERE id = ?`, claimID).Scan(ctx, &parent); err != nil {
		t.Fatalf("read parent: %v", err)
	}
	childID := uuid.New()
	rec := postRoutedOrganization(t, appID, &shared.Organization{
		ID:        childID,
		Name:      "Routed child " + childID.String(),
		OwnerID:   parent,
		RootID:    parent,
		CreatedBy: actor,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("post child: want 201, got %d %s", rec.Code, rec.Body.String())
	}

	foreign := uuid.New()
	if _, err := psdb.Instance().NewRaw(`
		INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
		VALUES (?, ?, 'entity', ?, ?, 0, ?)`, foreign, "Foreign root "+foreign.String(), foreign, foreign, actor).Exec(ctx); err != nil {
		t.Fatalf("insert foreign root: %v", err)
	}
	strayID := uuid.New()
	rec = postRoutedOrganization(t, appID, &shared.Organization{
		ID:        strayID,
		Name:      "Routed stray " + strayID.String(),
		OwnerID:   foreign,
		RootID:    foreign,
		CreatedBy: actor,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("post child under a foreign root: want 403, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestPutOrganizationClearsAFieldSentAsNull(t *testing.T) {
	appID, claimID := getGrantFixture(t)
	ctx := context.Background()
	var rootID uuid.UUID
	if err := psdb.Instance().NewRaw(`SELECT claim.sa_owner_id FROM claimius.claim WHERE id = ?`, claimID).Scan(ctx, &rootID); err != nil {
		t.Fatalf("read root: %v", err)
	}
	external := "ext-" + uuid.NewString()
	stored := func() *string {
		var v *string
		if err := psdb.Instance().NewRaw(`SELECT external_id FROM claimius.organization WHERE id = ?`, rootID).Scan(ctx, &v); err != nil {
			t.Fatalf("read external id: %v", err)
		}
		return v
	}
	getPatchStatus(pecho.PutOrganization, appID, claimID, rootID, `{"external_id":"`+external+`"}`)
	if v := stored(); v == nil || *v != external {
		t.Fatalf("an external id sent must be stored, got %v", v)
	}
	getPatchStatus(pecho.PutOrganization, appID, claimID, rootID, `{"external_id":null}`)
	if v := stored(); v != nil {
		t.Fatalf("an external id sent as null must be cleared, got %q", *v)
	}
	if code, _ := getPatchStatus(pecho.PutOrganization, appID, claimID, rootID, `{}`); code != http.StatusBadRequest {
		t.Fatalf("an empty patch must be refused with 400, got %d", code)
	}
}

func TestPutOrganizationMovesUnderASiblingAndRefusesACycle(t *testing.T) {
	appID, claimID := getGrantFixture(t)
	ctx := context.Background()
	mover, sibling, child := uuid.New(), uuid.New(), uuid.New()
	if _, err := psdb.Instance().ExecContext(ctx, `
		INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
		VALUES (?, 'Mover', 'entity', ?, ?, 1, ?), (?, 'Sibling', 'entity', ?, ?, 1, ?)`,
		mover, grantRoot, grantRoot, grantUser, sibling, grantRoot, grantRoot, grantUser); err != nil {
		t.Fatalf("insert siblings: %v", err)
	}
	if _, err := psdb.Instance().ExecContext(ctx, `
		INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
		VALUES (?, 'Mover child', 'entity', ?, ?, 2, ?)`, child, mover, grantRoot, grantUser); err != nil {
		t.Fatalf("insert child: %v", err)
	}
	for _, id := range []uuid.UUID{mover, sibling, child} {
		if _, err := psdb.Instance().ExecContext(ctx, `SELECT claimius.upsert_app_object(?, 'claimius.organization', ?)`, appID, id); err != nil {
			t.Fatalf("link %s: %v", id, err)
		}
	}
	ancestors := func(id uuid.UUID) []uuid.UUID {
		ids := make([]uuid.UUID, 0)
		if err := psdb.Instance().NewRaw(`SELECT object_id FROM claimius.get_owner_ancestors('claimius.organization', ?) WHERE hop > 0`, id).Scan(ctx, &ids); err != nil {
			t.Fatalf("ancestors of %s: %v", id, err)
		}
		return ids
	}

	if code, body := getPatchStatus(pecho.PutOrganization, appID, claimID, mover, `{"owner_id":"`+sibling.String()+`"}`); code == http.StatusBadRequest || code == http.StatusNotFound {
		t.Fatalf("a move under a sibling must pass, got %d %s", code, body)
	}
	if got := ancestors(child); !slices.Contains(got, sibling) || !slices.Contains(got, mover) {
		t.Fatalf("the child of a moved organization must follow it under the sibling, got ancestors %v", got)
	}
	if code, _ := getPatchStatus(pecho.PutOrganization, appID, claimID, sibling, `{"owner_id":"`+child.String()+`"}`); code != http.StatusBadRequest {
		t.Fatalf("a move under a descendant must be refused with 400, got %d", code)
	}
	if code, _ := getPatchStatus(pecho.PutOrganization, appID, claimID, sibling, `{"owner_id":"`+sibling.String()+`"}`); code != http.StatusBadRequest {
		t.Fatalf("a move under itself must be refused with 400, got %d", code)
	}
	if code, _ := getPatchStatus(pecho.PutOrganization, appID, claimID, sibling, `{"owner_id":"`+uuid.Nil.String()+`"}`); code != http.StatusBadRequest {
		t.Fatalf("a move under the nil uuid must be refused with 400, got %d", code)
	}
	var owner uuid.UUID
	if err := psdb.Instance().NewRaw(`SELECT sa_owner_id FROM claimius.organization WHERE id = ?`, sibling).Scan(ctx, &owner); err != nil {
		t.Fatalf("read owner: %v", err)
	}
	if owner != grantRoot {
		t.Fatalf("a refused move must leave the owner, got %s", owner)
	}
}

func TestRootAccessReadsTheRootOnTheProphet(t *testing.T) {
	_, claimID := getGrantFixture(t)
	ctx := context.Background()
	var rootID uuid.UUID
	if err := psdb.Instance().NewRaw(`SELECT sa_root_id FROM claimius.claim WHERE id = ?`, claimID).Scan(ctx, &rootID); err != nil {
		t.Fatalf("read root: %v", err)
	}
	ac, err := psdb.Instance().RootAccess(ctx, grantUser, shared.ACCESS_READ)
	if err != nil {
		t.Fatalf("root access: %v", err)
	}
	if ac.RootID != rootID {
		t.Fatalf("root access: want a grant under root %s, got %+v", rootID, ac)
	}
}
