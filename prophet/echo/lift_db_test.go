//go:build dbtest

package echo_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/disciple/sam"
	psam "github.com/nimling/samna-auth-middleware/v2/prophet/sam"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/smw"
	"github.com/uptrace/bun"
)

func TestADiscipleLiftsItsDataIntoTheProphetOnce(t *testing.T) {
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

	rootID, userID, claimID, grantID, bindingID, tokenID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	external := "ext-" + uuid.NewString()
	if err := disciple.RunInTx(ctx, nil, func(c context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(c, `SET LOCAL claimius.replay_mode = 'true'`); err != nil {
			return err
		}
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by) VALUES (?, 'Lifted root', 'company', ?, ?, 0, ?)`, []any{rootID, rootID, rootID, grantID}},
			{`INSERT INTO claimius.samna_user (id, user_id, first_name, last_name, email, status, type) VALUES (?, ?, 'Lifted', 'User', ?, 'active', 'user')`, []any{uuid.New(), userID, external + "@example.com"}},
			{`INSERT INTO claimius.claim (id, name, sa_access, inherits, type, sa_owner_id, sa_root_id, sa_created_by) VALUES (?, 'Lifted owner', 15, TRUE, 'user', ?, ?, ?)`, []any{claimID, rootID, rootID, grantID}},
			{`INSERT INTO claimius.user_claim (id, claim_id, user_id, sa_owner_id, sa_created_by) VALUES (?, ?, ?, ?, ?)`, []any{grantID, claimID, userID, rootID, grantID}},
			{`INSERT INTO claimius.claim_object (id, claim_id, object_id, object_type, inherits, sa_owner_id, sa_root_id, sa_created_by) VALUES (?, ?, ?, 'claimius.organization', TRUE, ?, ?, ?)`, []any{bindingID, claimID, rootID, rootID, rootID, grantID}},
			{`INSERT INTO claimius.user_token (id, user_id, external_id, type, sa_owner_id, sa_created_by) VALUES (?, ?, ?, 'external', ?, ?)`, []any{tokenID, userID, external, rootID, grantID}},
		} {
			if _, err := tx.ExecContext(c, q.sql, q.args...); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("disciple rows: %v", err)
	}

	replica := &sam.Replica{DB: disciple}
	result, err := replica.Lift(ctx)
	if err != nil {
		t.Fatalf("a disciple must lift its data into the prophet, got %v", err)
	}
	if result.Lifted["claimius.organization"] != 1 || result.Lifted["claimius.claim"] != 1 || result.Lifted["claimius.user_token"] != 1 {
		t.Fatalf("the lift must count every row it lifted, got %v", result.Lifted)
	}

	var linked bool
	if err := prophet.NewRaw(`SELECT claimius.app_has_object(?, 'claimius.organization', ?)`, app.ID, rootID).Scan(ctx, &linked); err != nil || !linked {
		t.Fatalf("a lifted organization must be linked to the app, got %v %v", linked, err)
	}
	var claimApp, tokenApp, userApp uuid.UUID
	if err := prophet.NewRaw(`SELECT app_id FROM claimius.claim WHERE id = ?`, claimID).Scan(ctx, &claimApp); err != nil || claimApp != app.ID {
		t.Fatalf("a lifted claim must belong to the app, got %s %v", claimApp, err)
	}
	if err := prophet.NewRaw(`SELECT app_id FROM claimius.user_token WHERE id = ?`, tokenID).Scan(ctx, &tokenApp); err != nil || tokenApp != app.ID {
		t.Fatalf("a lifted identity must belong to the app, got %s %v", tokenApp, err)
	}
	if err := prophet.NewRaw(`SELECT app_id FROM claimius.samna_user WHERE user_id = ? AND app_id = ?`, userID, app.ID).Scan(ctx, &userApp); err != nil {
		t.Fatalf("a lifted user must have its row in the app: %v", err)
	}
	var grantsOnProphet int
	if err := prophet.NewRaw(`SELECT count(*) FROM claimius.user_claim WHERE id = ? AND app_id = ?`, grantID, app.ID).Scan(ctx, &grantsOnProphet); err != nil || grantsOnProphet != 1 {
		t.Fatalf("a lifted grant must be on the prophet, got %d %v", grantsOnProphet, err)
	}

	state, err := replica.GetState(ctx)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	head := getAppHead(t, app.ID)
	if state.Cursor.IsZero() || state.Cursor.Less(head) {
		t.Fatalf("after a lift the disciple's cursor must stand at the prophet's position %s, got %s", head, state.Cursor)
	}
	if state.Prophet == "" {
		t.Fatal("after a lift the disciple must be bound to the prophet that took its data")
	}

	if _, err := replica.Lift(ctx); err == nil || !strings.Contains(strings.ToLower(err.Error()), "conflict") {
		t.Fatalf("a second lift must stop on the ids the prophet already holds, got %v", err)
	}
	var claims int
	if err := prophet.NewRaw(`SELECT count(*) FROM claimius.claim WHERE id = ?`, claimID).Scan(ctx, &claims); err != nil || claims != 1 {
		t.Fatalf("a refused lift must leave the prophet as it was, got %d %v", claims, err)
	}
}
