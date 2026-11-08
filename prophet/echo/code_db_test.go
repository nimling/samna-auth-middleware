//go:build dbtest

package echo_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/disciple/sam"
	psam "github.com/nimling/samna-auth-middleware/v2/prophet/sam"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/smw"
)

func TestCodesAreWrittenOnTheProphetAndResolvedOnTheDisciple(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	prophet := psdb.Instance()
	app := postPeerApp(t, grantUser)

	claim := func(name string) uuid.UUID {
		var id uuid.UUID
		if err := prophet.NewRaw(`
			SELECT (claimius.insert_claim(p_app_id => ?, p_name => ?, p_description => NULL, p_sa_access => 15,
			        p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?) -> 'claim' ->> 'id')::UUID
		`, app.ID, name, app.Org, app.Org, grantUser).Scan(ctx, &id); err != nil {
			t.Fatalf("claim %s: %v", name, err)
		}
		if _, err := prophet.ExecContext(ctx, `
			SELECT claimius.insert_claim_object(p_app_id => ?, p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
			       p_sa_created_by => ?, p_object_id => ?, p_object_type => 'claimius.organization', p_inherits => TRUE)
		`, app.ID, id, app.Org, app.Org, grantUser, app.Org); err != nil {
			t.Fatalf("bind %s: %v", name, err)
		}
		return id
	}
	owners, readers := claim("Code owners"), claim("Code readers")
	owner, invited := uuid.New(), uuid.New()
	for _, u := range []uuid.UUID{owner, invited} {
		if _, err := prophet.ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Code user')`, u, app.ID); err != nil {
			t.Fatalf("user: %v", err)
		}
	}
	if _, err := prophet.ExecContext(ctx, `SELECT claimius.insert_user_claim(p_app_id => ?, p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?)`,
		app.ID, owners, owner, app.Org, grantUser); err != nil {
		t.Fatalf("grant owner: %v", err)
	}

	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("request_id", uuid.New())
			return next(c)
		}
	})
	psam.RegisterEndpoints(e.Group("/sam", smw.AppMiddleware))
	server := httptest.NewServer(e)
	defer server.Close()

	disciple := getDiscipleDB(t)
	if _, err := disciple.ExecContext(ctx, `SELECT claimius.upsert_disciple_state(?)`, app.Slug); err != nil {
		t.Fatalf("disciple state: %v", err)
	}
	t.Cleanup(func() {
		if _, err := disciple.ExecContext(context.Background(), `UPDATE claimius.disciple_state SET prophet = NULL, endpoint = NULL`); err != nil {
			t.Errorf("unbind the disciple: %v", err)
		}
	})
	t.Setenv("SAUTH_ENDPOINT", server.URL)
	t.Setenv("SAUTH_APP_SLUG", app.Slug)
	t.Setenv("SAUTH_APP_SECRET", app.Secret)
	replica := &sam.Replica{DB: disciple}
	if err := replica.PullSnapshot(ctx, "", ""); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	disciple.RouteToProphet = true
	defer func() { disciple.RouteToProphet = false }()

	ac, err := disciple.MutationAccess(ctx, owner, readers, shared.OBJECT_TYPE_CLAIM, shared.ACCESS_OWNER)
	if err != nil {
		t.Fatalf("the owner owns the reader claim on the disciple: %v", err)
	}
	if _, err := disciple.PutClaimGuest(ctx, ac, readers, shared.GUEST_MODE_CODE); err != nil {
		t.Fatalf("the disciple makes the claim a guest claim through the prophet: %v", err)
	}
	guest := psdb.GetGuestUserID(app.ID, readers)
	for name, db := range map[string]*shared.DB{"prophet": prophet, "disciple": disciple} {
		var mode string
		if err := db.NewRaw(`SELECT guest_mode FROM claimius.claim WHERE id = ?`, readers).Scan(ctx, &mode); err != nil || mode != "code" {
			t.Fatalf("the %s holds guest_mode code, got %q %v", name, mode, err)
		}
		var grants int
		if err := db.NewRaw(`SELECT count(*) FROM claimius.user_claim WHERE claim_id = ? AND user_id = ? AND sa_deleted_at IS NULL`, readers, guest).Scan(ctx, &grants); err != nil || grants != 1 {
			t.Fatalf("the %s holds the grant of the claim's guest, got %d %v", name, grants, err)
		}
	}

	stranger := &shared.AccessClaim{UserID: invited}
	if _, err := disciple.PostCode(ctx, stranger, readers, shared.BodyCode{Kind: shared.CODE_KIND_GUEST}, ""); err == nil {
		t.Fatal("the prophet refuses a code from a user who does not own the claim")
	}

	created, err := disciple.PostCode(ctx, ac, readers, shared.BodyCode{Kind: shared.CODE_KIND_GUEST, Count: 1}, "https://svc.example.test")
	if err != nil {
		t.Fatalf("post code through the prophet: %v", err)
	}
	var stored int
	if err := prophet.NewRaw(`SELECT count(*) FROM claimius.code WHERE id = ? AND app_id = ? AND hash = ?`, created.ID, app.ID, shared.GetCodeHash(created.Value)).Scan(ctx, &stored); err != nil || stored != 1 {
		t.Fatalf("the prophet stores the code by its hash, got %d %v", stored, err)
	}
	access, err := disciple.GetCodeAccess(ctx, created.Value)
	if err != nil {
		t.Fatalf("the disciple resolves the code it was answered with: %v", err)
	}
	if access.GuestUserID == nil || *access.GuestUserID != guest {
		t.Fatalf("the code resolves to the claim's guest %s, got %v", guest, access.GuestUserID)
	}
	if _, err := disciple.PutCodeUsed(ctx, created.Value); err != nil {
		t.Fatalf("the counted use is taken on the prophet: %v", err)
	}
	if _, err := disciple.PutCodeUsed(ctx, created.Value); err != shared.ErrCodeInvalid {
		t.Fatalf("a single use code is used up on the prophet, got %v", err)
	}
	var used int
	if err := disciple.NewRaw(`SELECT used_count FROM claimius.code WHERE id = ?`, created.ID).Scan(ctx, &used); err != nil || used != 1 {
		t.Fatalf("the disciple holds the prophet's used_count, got %d %v", used, err)
	}

	invite, err := disciple.PostCode(ctx, ac, readers, shared.BodyCode{Kind: shared.CODE_KIND_INVITE, Count: 1}, "")
	if err != nil {
		t.Fatalf("post invite: %v", err)
	}
	if _, err := disciple.PostCodeRedeem(ctx, invite.Value, invited); err != nil {
		t.Fatalf("redeem on the prophet: %v", err)
	}
	for name, db := range map[string]*shared.DB{"prophet": prophet, "disciple": disciple} {
		var grants int
		if err := db.NewRaw(`SELECT count(*) FROM claimius.user_claim WHERE claim_id = ? AND user_id = ? AND sa_deleted_at IS NULL`, readers, invited).Scan(ctx, &grants); err != nil || grants != 1 {
			t.Fatalf("the %s holds the grant the invite wrote, got %d %v", name, grants, err)
		}
	}

	if _, err := disciple.DeleteCode(ctx, ac, invite.ID); err != nil {
		t.Fatalf("revoke on the prophet: %v", err)
	}
	var revoked bool
	if err := disciple.NewRaw(`SELECT revoked_at IS NOT NULL FROM claimius.code WHERE id = ?`, invite.ID).Scan(ctx, &revoked); err != nil || !revoked {
		t.Fatalf("the disciple holds the revoke, got %v %v", revoked, err)
	}

	if _, err := disciple.ExecContext(ctx, `DELETE FROM claimius.code WHERE claim_id = ?`, readers); err != nil {
		t.Fatalf("drop the local codes: %v", err)
	}
	if err := replica.PullSnapshot(ctx, "", ""); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	var snapped int
	if err := disciple.NewRaw(`SELECT count(*) FROM claimius.code WHERE claim_id = ? AND hash <> ''`, readers).Scan(ctx, &snapped); err != nil || snapped != 2 {
		t.Fatalf("the snapshot carries the app's codes with their hashes, got %d %v", snapped, err)
	}
}
