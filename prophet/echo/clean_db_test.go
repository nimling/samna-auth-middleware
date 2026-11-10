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
	"github.com/nimling/samna-auth-middleware/v2/smw"
)

func TestTheCleanRemovesWhatARoutedWriteLeftOnAnObjectTheHostNeverCommitted(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	prophet := psdb.Instance()
	app := postPeerApp(t, grantUser)

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

	claim := func(name string) uuid.UUID {
		var id uuid.UUID
		if err := prophet.NewRaw(`
			SELECT (claimius.insert_claim(p_name => ?, p_description => NULL, p_sa_access => 15,
			        p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?, p_type => 'virtual') -> 'claim' ->> 'id')::UUID
		`, name, app.Org, app.Org, grantUser).Scan(ctx, &id); err != nil {
			t.Fatalf("claim %s: %v", name, err)
		}
		return id
	}
	bind := func(claimID, objectID uuid.UUID) uuid.UUID {
		var id uuid.UUID
		if err := prophet.NewRaw(`
			SELECT (claimius.insert_claim_object(p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
			       p_sa_created_by => ?, p_object_id => ?, p_object_type => 'public.gotest_consumer_row', p_inherits => TRUE) -> 'claim_object' ->> 'id')::UUID
		`, claimID, app.Org, app.Org, grantUser, objectID).Scan(ctx, &id); err != nil {
			t.Fatalf("bind: %v", err)
		}
		return id
	}
	kept, lost := uuid.New(), uuid.New()
	if _, err := disciple.ExecContext(ctx, `INSERT INTO public.gotest_consumer_row (id, name, sa_owner_id) VALUES (?, 'Committed', ?)`, kept, app.Org); err != nil {
		t.Fatalf("committed row: %v", err)
	}
	keptClaim, lostClaim := claim("Committed owner"), claim("Lost owner")
	keptBinding, lostBinding := bind(keptClaim, kept), bind(lostClaim, lost)
	if _, err := prophet.ExecContext(ctx, `UPDATE claimius.claim SET sa_created_at = now() - interval '1 hour' WHERE id IN (?, ?)`, keptClaim, lostClaim); err != nil {
		t.Fatalf("age claims: %v", err)
	}
	if _, err := prophet.ExecContext(ctx, `UPDATE claimius.claim_object SET sa_created_at = now() - interval '1 hour' WHERE id IN (?, ?)`, keptBinding, lostBinding); err != nil {
		t.Fatalf("age bindings: %v", err)
	}

	replica := &sam.Replica{DB: disciple}
	if err := replica.PullSnapshot(ctx, "", ""); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	disciple.RouteToProphet = true
	defer func() { disciple.RouteToProphet = false }()

	removed, err := replica.CleanOrphans(ctx)
	if err != nil {
		t.Fatalf("clean: %v", err)
	}
	if removed != 2 {
		t.Fatalf("the clean must remove the lost binding and the claim it leaves without bindings, got %d", removed)
	}
	var live bool
	if err := prophet.NewRaw(`SELECT sa_deleted_at IS NULL FROM claimius.claim_object WHERE id = ?`, lostBinding).Scan(ctx, &live); err != nil || live {
		t.Fatalf("a binding on an object the host never committed must be gone from the prophet, got live=%v %v", live, err)
	}
	if err := prophet.NewRaw(`SELECT sa_deleted_at IS NULL FROM claimius.claim WHERE id = ?`, lostClaim).Scan(ctx, &live); err != nil || live {
		t.Fatalf("the virtual claim left without bindings must be gone from the prophet, got live=%v %v", live, err)
	}
	if err := prophet.NewRaw(`SELECT sa_deleted_at IS NULL FROM claimius.claim_object WHERE id = ?`, keptBinding).Scan(ctx, &live); err != nil || !live {
		t.Fatalf("a binding on a committed object must stay, got live=%v %v", live, err)
	}
	if removed, err := replica.CleanOrphans(ctx); err != nil || removed != 0 {
		t.Fatalf("a second clean must find nothing, got %d %v", removed, err)
	}
}
