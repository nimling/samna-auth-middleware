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

func TestRoutedDeleteRemovesAScopeBindingAndAGrantByTheirOwnID(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	prophet := psdb.Instance()
	app := postPeerApp(t, grantUser)

	var claimID uuid.UUID
	if err := prophet.NewRaw(`
		SELECT (claimius.insert_claim(p_name => 'Scoped pages', p_description => NULL, p_sa_access => 4,
		        p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?) -> 'claim' ->> 'id')::UUID
	`, app.Org, app.Org, grantUser).Scan(ctx, &claimID); err != nil {
		t.Fatalf("claim: %v", err)
	}
	var bindingID uuid.UUID
	if err := prophet.NewRaw(`
		SELECT (claimius.insert_claim_object(p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
		       p_sa_created_by => ?, p_scope => '{"page": {"hr": {"_access": "READ"}}}'::JSONB) -> 'claim_object' ->> 'id')::UUID
	`, claimID, app.Org, app.Org, grantUser).Scan(ctx, &bindingID); err != nil {
		t.Fatalf("scope binding: %v", err)
	}
	member := uuid.New()
	if _, err := prophet.ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Member')`, member, app.ID); err != nil {
		t.Fatalf("member: %v", err)
	}
	var grantID uuid.UUID
	if err := prophet.NewRaw(`
		SELECT (claimius.insert_user_claim(p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?) -> 'user_claim' ->> 'id')::UUID
	`, claimID, member, app.Org, grantUser).Scan(ctx, &grantID); err != nil {
		t.Fatalf("grant: %v", err)
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
	t.Cleanup(func() { disciple.RouteToProphet = false })

	live := func(table string, id uuid.UUID) (bool, bool) {
		t.Helper()
		var onProphet, onDisciple bool
		query := `SELECT EXISTS (SELECT 1 FROM claimius.` + table + ` WHERE id = ? AND sa_deleted_at IS NULL)`
		if err := prophet.NewRaw(query, id).Scan(ctx, &onProphet); err != nil {
			t.Fatalf("prophet %s: %v", table, err)
		}
		if err := disciple.NewRaw(query, id).Scan(ctx, &onDisciple); err != nil {
			t.Fatalf("disciple %s: %v", table, err)
		}
		return onProphet, onDisciple
	}
	if p, d := live("claim_object", bindingID); !p || !d {
		t.Fatalf("the scope binding must be live on both before the delete, prophet %v disciple %v", p, d)
	}

	actor := &shared.AccessClaim{UserID: grantUser}
	if _, err := disciple.NewDelete().Model(&shared.ClaimObject{ID: bindingID, ClaimID: claimID}).Access(actor).Exec(ctx); err != nil {
		t.Fatalf("routed scope binding delete: %v", err)
	}
	if p, d := live("claim_object", bindingID); p || d {
		t.Fatalf("a scope binding deleted by its id is gone on both, prophet %v disciple %v", p, d)
	}

	if _, err := disciple.NewDelete().Model(&shared.UserClaim{ID: grantID, ClaimID: claimID}).Access(actor).Exec(ctx); err != nil {
		t.Fatalf("routed grant delete: %v", err)
	}
	if p, d := live("user_claim", grantID); p || d {
		t.Fatalf("a grant deleted by its id is gone on both, prophet %v disciple %v", p, d)
	}
}
