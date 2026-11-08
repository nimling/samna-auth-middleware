//go:build dbtest

package echo_test

import (
	"context"
	"errors"
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

func TestADiscipleBindsOnItsOwnRowThroughTheProphet(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	prophet := psdb.Instance()
	app := postPeerApp(t, grantUser)
	outside, branch := uuid.New(), uuid.New()
	if _, err := prophet.ExecContext(ctx, `
		INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
		VALUES (?, 'Unlinked root', 'company', ?, ?, 0, ?), (?, 'Linked branch', 'company', ?, ?, 1, ?)
	`, outside, outside, outside, grantUser, branch, outside, outside, grantUser); err != nil {
		t.Fatalf("organizations: %v", err)
	}
	if _, err := prophet.ExecContext(ctx, `SELECT claimius.upsert_app_object(?, 'claimius.organization', ?, ?)`, app.ID, branch, grantUser); err != nil {
		t.Fatalf("link branch: %v", err)
	}

	claim := func(name string, access int) uuid.UUID {
		var id uuid.UUID
		if err := prophet.NewRaw(`
			SELECT (claimius.insert_claim(p_app_id => ?, p_name => ?, p_description => NULL, p_sa_access => ?,
			        p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?) -> 'claim' ->> 'id')::UUID
		`, app.ID, name, access, branch, outside, grantUser).Scan(ctx, &id); err != nil {
			t.Fatalf("claim %s: %v", name, err)
		}
		return id
	}
	owners, viewers, rowOwners := claim("Branch owners", 15), claim("Branch viewers", 4), claim("Row owners", 15)
	for _, c := range []uuid.UUID{owners, viewers} {
		if _, err := prophet.ExecContext(ctx, `
			SELECT claimius.insert_claim_object(p_app_id => ?, p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
			       p_sa_created_by => ?, p_object_id => ?, p_object_type => 'claimius.organization', p_inherits => TRUE)
		`, app.ID, c, branch, outside, grantUser, branch); err != nil {
			t.Fatalf("bind on branch: %v", err)
		}
	}
	owner, viewer := uuid.New(), uuid.New()
	var ownerGrant, viewerGrant uuid.UUID
	for _, g := range []struct {
		user  uuid.UUID
		claim uuid.UUID
		out   *uuid.UUID
	}{{owner, owners, &ownerGrant}, {viewer, viewers, &viewerGrant}} {
		var id uuid.UUID
		if err := prophet.NewRaw(`
			SELECT (claimius.insert_user_claim(p_app_id => ?, p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?)
			        -> 'user_claim' ->> 'id')::UUID
			FROM claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Branch user')
		`, app.ID, g.claim, g.user, branch, grantUser, g.user, app.ID).Scan(ctx, &id); err != nil {
			t.Fatalf("grant: %v", err)
		}
		if g.out != nil {
			*g.out = id
		}
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
	if err := (&sam.Replica{DB: disciple}).PullSnapshot(ctx, "", ""); err != nil {
		t.Fatalf("a snapshot carrying claims rooted at an organization outside the app must apply, got %v", err)
	}
	var rooted int
	if err := disciple.NewRaw(`SELECT count(*) FROM claimius.claim WHERE sa_root_id = ?`, outside).Scan(ctx, &rooted); err != nil || rooted != 3 {
		t.Fatalf("the disciple must hold the three claims rooted outside the app, got %d %v", rooted, err)
	}

	row := uuid.New()
	if _, err := disciple.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS public.gotest_consumer_row (
			id UUID PRIMARY KEY, name TEXT, sa_owner_id UUID NOT NULL, sa_created_by UUID,
			sa_deleted_at TIMESTAMPTZ, sa_created_at TIMESTAMPTZ NOT NULL DEFAULT now(), sa_updated_at TIMESTAMPTZ NOT NULL DEFAULT now())
	`); err != nil {
		t.Fatalf("consumer table: %v", err)
	}
	if _, err := disciple.ExecContext(ctx, `
		SELECT claimius.upsert_table_info(VARIADIC (SELECT coalesce(array_agg(object_type), '{}') FROM claimius.table_info WHERE NOT is_internal AND object_type <> 'public.gotest_consumer_row') || 'public.gotest_consumer_row'::TEXT)
	`); err != nil {
		t.Fatalf("register consumer table: %v", err)
	}
	booked := uuid.New()
	if _, err := disciple.ExecContext(ctx, `INSERT INTO public.gotest_consumer_row (id, name, sa_owner_id, sa_created_by) VALUES (?, 'Row', ?, ?), (?, 'Booked', ?, ?)`,
		row, branch, ownerGrant, booked, branch, viewerGrant); err != nil {
		t.Fatalf("consumer row: %v", err)
	}

	disciple.RouteToProphet = true
	rowType := shared.ObjectType("public.gotest_consumer_row")
	bind := func(actor uuid.UUID, required shared.ClaimAccess) error {
		ac, err := disciple.MutationAccess(ctx, actor, branch, string(shared.OBJECT_TYPE_ORGANIZATION), required)
		if err != nil {
			t.Fatalf("access on the branch: %v", err)
		}
		return disciple.NewInsert().Model(&shared.ClaimObject{
			ClaimID: rowOwners, ObjectID: &row, ObjectType: &rowType, Inherits: true, OwnerID: branch, RootID: outside,
		}).Access(ac).Scan(ctx)
	}

	if err := bind(owner, shared.ACCESS_OWNER); err != nil {
		t.Fatalf("an owner of the branch must bind an owner claim on the disciple's own row through the prophet, got %v", err)
	}
	var stored int
	if err := prophet.NewRaw(`SELECT count(*) FROM claimius.claim_object WHERE claim_id = ? AND object_type = ? AND object_id = ?`,
		rowOwners, string(rowType), row).Scan(ctx, &stored); err != nil || stored != 1 {
		t.Fatalf("the prophet must store the binding with its type untouched, got %d %v", stored, err)
	}
	if err := bind(viewer, shared.ACCESS_READ); !errors.Is(err, shared.ErrNoAccess) {
		t.Fatalf("a reader of the branch must be refused owner on a row created under another user's grant with ErrNoAccess, got %v", err)
	}

	ac, err := disciple.MutationAccess(ctx, viewer, branch, string(shared.OBJECT_TYPE_ORGANIZATION), shared.ACCESS_READ)
	if err != nil {
		t.Fatalf("access on the branch: %v", err)
	}
	if _, err := disciple.CreateVirtualClaim(ctx, nil, ac, shared.CreateVirtualClaimParams{
		Name: "Booked - owner", Access: shared.ACCESS_ALL | shared.ACCESS_MEMBER, OwnerID: branch,
		ObjectID: booked, ObjectType: string(rowType), TargetUserID: viewer,
	}); err != nil {
		t.Fatalf("a reader of the branch must take ownership of the row it created through the prophet, got %v", err)
	}
	if err := prophet.NewRaw(`SELECT count(*) FROM claimius.claim_object WHERE object_type = ? AND object_id = ? AND sa_created_by = ?`,
		string(rowType), booked, viewerGrant).Scan(ctx, &stored); err != nil || stored != 1 {
		t.Fatalf("the prophet must store the creator's binding on the row, got %d %v", stored, err)
	}

	forged := uuid.New()
	tx, err := prophet.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT set_config('claimius.object_created_by', ?, TRUE)`, ownerGrant.String()); err != nil {
		t.Fatalf("vouch: %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		SELECT claimius.insert_claim_object(p_app_id => ?, p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
		       p_sa_created_by => ?, p_object_id => ?, p_object_type => ?, p_inherits => TRUE)
	`, app.ID, rowOwners, branch, outside, viewerGrant, forged, string(rowType)); err == nil {
		t.Fatal("a creator grant that is not the actor's own must not make the actor the row's creator")
	}
}
