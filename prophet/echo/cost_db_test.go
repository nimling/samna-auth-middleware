//go:build dbtest

package echo_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/disciple/sam"
	dsdb "github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	psam "github.com/nimling/samna-auth-middleware/v2/prophet/sam"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/smw"
	"github.com/uptrace/bun"
)

var errCostRollback = errors.New("rollback")

func costNumber(v float64) *float64 { return &v }

func costAt(t *testing.T, value string) *time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("time %s: %v", value, err)
	}
	return &parsed
}

func TestADiscipleReportsItsCostThroughTheProphetAndReadsItBack(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	prophet := psdb.Instance()
	app := postPeerApp(t, grantUser)

	var owners uuid.UUID
	if err := prophet.NewRaw(`
		SELECT (claimius.insert_claim(p_app_id => ?, p_name => 'Cost owners', p_description => NULL, p_sa_access => 15,
		        p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?) -> 'claim' ->> 'id')::UUID
	`, app.ID, app.Org, app.Org, grantUser).Scan(ctx, &owners); err != nil {
		t.Fatalf("claim: %v", err)
	}
	for _, bound := range []struct {
		id    uuid.UUID
		otype string
	}{{app.Org, "claimius.organization"}, {app.ID, "claimius.samna_app"}} {
		if _, err := prophet.ExecContext(ctx, `
			SELECT claimius.insert_claim_object(p_app_id => ?, p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
			       p_sa_created_by => ?, p_object_id => ?, p_object_type => ?, p_inherits => TRUE)
		`, app.ID, owners, app.Org, app.Org, grantUser, bound.id, bound.otype); err != nil {
			t.Fatalf("bind %s: %v", bound.otype, err)
		}
	}
	owner, outsider := uuid.New(), uuid.New()
	var ownerGrant uuid.UUID
	if err := prophet.NewRaw(`
		SELECT (claimius.insert_user_claim(p_app_id => ?, p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?)
		        -> 'user_claim' ->> 'id')::UUID
		FROM claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Owner')
	`, app.ID, owners, owner, app.Org, grantUser, owner, app.ID).Scan(ctx, &ownerGrant); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if _, err := prophet.ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Outsider')`, outsider, app.ID); err != nil {
		t.Fatalf("outsider: %v", err)
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

	ac, err := disciple.MutationAccess(ctx, owner, app.Org, shared.OBJECT_TYPE_ORGANIZATION, shared.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("owner access: %v", err)
	}
	hour := costAt(t, "2026-10-30T09:00:00Z")
	next := costAt(t, "2026-10-30T10:00:00Z")
	first := &shared.AppCost{}
	insert := disciple.NewInsert().Model(&shared.AppCostInput{Module: app.Slug, PeriodStart: hour, PeriodEnd: next, Currency: "NOK", Ref: "h1",
		Items:  []shared.AppCostItem{{Description: "requests", Quantity: 10, Unit: "request", UnitPrice: costNumber(0.15)}},
		Detail: json.RawMessage(`{"subject": {"type": "claimius.organization", "id": "` + app.Org.String() + `"}}`)}).Access(ac)
	if err := insert.Scan(ctx, first); err != nil || !insert.Created() {
		t.Fatalf("the app's actor reports an hour through the prophet and gets 201, got %v created %v", err, insert.Created())
	}
	var stored struct {
		AppID     uuid.UUID       `bun:"app_id"`
		OwnerID   uuid.UUID       `bun:"sa_owner_id"`
		CreatedBy *uuid.UUID      `bun:"sa_created_by"`
		Kind      string          `bun:"kind"`
		Quantity  float64         `bun:"quantity"`
		Amount    float64         `bun:"amount"`
		SchemaID  string          `bun:"schema_id"`
		Detail    json.RawMessage `bun:"detail"`
	}
	if err := prophet.NewRaw(`SELECT app_id, sa_owner_id, sa_created_by, kind, quantity, amount, schema ->> '$id' AS schema_id, detail FROM claimius.app_cost WHERE id = ?`, first.ID).Scan(ctx, &stored); err != nil {
		t.Fatalf("read the stored row: %v", err)
	}
	if stored.AppID != app.ID || stored.OwnerID != app.Org || stored.CreatedBy == nil || *stored.CreatedBy != ownerGrant || stored.Kind != "usage" || stored.Quantity != 10 || stored.Amount != 1.5 ||
		stored.SchemaID != shared.COST_SCHEMA_ID || !strings.Contains(string(stored.Detail), `"amount": 1.5`) {
		t.Fatalf("the prophet stamps the calling app, its organization and the actor's grant, and normalises the items form, got %+v %s", stored, stored.Detail)
	}
	var local int
	if err := disciple.NewRaw(`SELECT count(*) FROM claimius.app_cost WHERE id = ? AND schema ->> '$id' = ?`, first.ID, shared.COST_SCHEMA_ID).Scan(ctx, &local); err != nil || local != 1 {
		t.Fatalf("the prophet's answer is copied onto the disciple with its schema, got %d %v", local, err)
	}

	again := &shared.AppCost{}
	replace := disciple.NewInsert().Model(&shared.AppCostInput{Module: app.Slug, Kind: "usage", PeriodStart: hour, PeriodEnd: next, Quantity: costNumber(25), Unit: "request", Amount: costNumber(3), Currency: "NOK", Ref: "h1"}).Access(ac)
	if err := replace.Scan(ctx, again); err != nil || replace.Created() {
		t.Fatalf("a rerun of the same ref is a replace answered 200, got %v created %v", err, replace.Created())
	}
	var live int
	if err := prophet.NewRaw(`SELECT count(*) FROM claimius.app_cost WHERE app_id = ? AND module = ? AND sa_deleted_at IS NULL`, app.ID, app.Slug).Scan(ctx, &live); err != nil || live != 1 || again.ID != first.ID || again.Quantity != 25 || string(again.Schema) != `{}` {
		t.Fatalf("a ref reported twice is one row replaced, got %d rows %+v %v", live, again, err)
	}
	if _, err := dsdb.PutAppCost(ctx, disciple, ac, &shared.AppCostInput{Module: app.Slug, Kind: "usage", PeriodStart: next, PeriodEnd: costAt(t, "2026-10-30T11:00:00Z"), Quantity: costNumber(5), Unit: "request", Amount: costNumber(0.5), Currency: "NOK", Ref: "h2"}); err != nil {
		t.Fatalf("report the next hour: %v", err)
	}
	if _, err := dsdb.PutAppCost(ctx, disciple, ac, &shared.AppCostInput{Module: "uptime", At: costAt(t, "2026-10-30T09:30:00Z"), Amount: costNumber(0.06), Currency: "NOK", Ref: "check-1"}); err != nil {
		t.Fatalf("report a built in module's event: %v", err)
	}
	correction, err := dsdb.PutAppCost(ctx, disciple, ac, &shared.AppCostInput{Module: app.Slug, Kind: "usage", PeriodStart: hour, PeriodEnd: next, Quantity: costNumber(30), Unit: "request", Amount: costNumber(4), Currency: "NOK", Ref: "h1-fix", SupersedesID: &first.ID})
	if err != nil || correction.SupersedesID == nil || *correction.SupersedesID != first.ID {
		t.Fatalf("a correction supersedes the first row through the prophet, got %+v %v", correction, err)
	}
	foreign := uuid.New()
	if _, err := dsdb.PutAppCost(ctx, disciple, ac, &shared.AppCostInput{Module: app.Slug, Kind: "usage", PeriodStart: hour, PeriodEnd: next, Quantity: costNumber(1), Unit: "request", Amount: costNumber(1), Currency: "NOK", Ref: "h1-bad", SupersedesID: &foreign}); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("a correction of a row the app does not hold is refused, got %v", err)
	}
	if _, err := dsdb.PutAppCost(ctx, disciple, ac, &shared.AppCostInput{Module: app.Slug, Kind: "usage", PeriodStart: hour, PeriodEnd: next, Quantity: costNumber(1), Unit: "request", Amount: costNumber(1), Currency: "nok", Ref: "h3"}); !errors.Is(err, shared.ErrBadAppCost) {
		t.Fatalf("a bad input is refused before it travels, got %v", err)
	}
	if _, err := dsdb.PutAppCost(ctx, disciple, ac, &shared.AppCostInput{Module: app.Slug, Kind: "usage", PeriodStart: hour, PeriodEnd: next, Quantity: costNumber(1), Unit: "request", Amount: costNumber(1), Currency: "NOK", Ref: "h3",
		Schema: json.RawMessage(`{"type": "object", "required": ["serial"]}`), Detail: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("the prophet refuses detail its schema does not accept")
	}

	if _, err := dsdb.PutAppCost(ctx, disciple, &shared.AccessClaim{UserID: outsider, OwnerID: app.Org, UserClaimID: &ownerGrant},
		&shared.AppCostInput{Module: app.Slug, At: hour, Amount: costNumber(1), Currency: "NOK", Ref: "forged"}); !errors.Is(err, shared.ErrNoAccess) {
		t.Fatalf("a write stamped with a grant the actor does not hold is refused by the prophet, got %v", err)
	}

	if _, err := disciple.NewUpdate().Model(&shared.AppCost{ID: correction.ID, Status: "invoiced"}).Access(ac).Column("status").Exec(ctx); err != nil {
		t.Fatalf("patch the status: %v", err)
	}
	var status string
	if err := prophet.NewRaw(`SELECT status FROM claimius.app_cost WHERE id = ?`, correction.ID).Scan(ctx, &status); err != nil || status != "invoiced" {
		t.Fatalf("a routed patch sets the status on the prophet, got %s %v", status, err)
	}
	if _, err := disciple.NewUpdate().Model(&shared.AppCost{ID: first.ID, Detail: json.RawMessage(`{"invoice": 7}`)}).Access(ac).Column("detail").Exec(ctx); err != nil {
		t.Fatalf("a patch of detail under an empty schema is free, got %v", err)
	}

	if _, err := replica.PullSync(ctx); err != nil {
		t.Fatalf("pull: %v", err)
	}
	hours, err := shared.GetAppCost(ctx, disciple, owner, uuid.Nil, shared.AppCostQuery{})
	if err != nil || len(hours) != 3 {
		t.Fatalf("the rows reach the disciple and its reader with the superseded one left out, got %v %v", hours, err)
	}
	days, err := shared.GetAppCost(ctx, disciple, owner, uuid.Nil, shared.AppCostQuery{Module: app.Slug, Granularity: "day"})
	if err != nil || len(days) != 1 || days[0].Quantity != 35 || days[0].Amount != 4.5 || days[0].Rows != 2 || days[0].Hours != 2 {
		t.Fatalf("a day bucket on the disciple folds the app's live rows, got %v %v", days, err)
	}
	if hidden, err := shared.GetAppCost(ctx, disciple, outsider, uuid.Nil, shared.AppCostQuery{}); err != nil || len(hidden) != 0 {
		t.Fatalf("a reader without read on the organization sees no cost, got %v %v", hidden, err)
	}
	onProphet, err := shared.GetAppCost(ctx, prophet, owner, app.ID, shared.AppCostQuery{Granularity: "day"})
	if err != nil || len(onProphet) != 2 || onProphet[0].AppID != app.ID {
		t.Fatalf("the prophet buckets the app's cost for a reader with read on the app, got %v %v", onProphet, err)
	}
	if hidden, err := shared.GetAppCost(ctx, prophet, outsider, app.ID, shared.AppCostQuery{}); err != nil || len(hidden) != 0 {
		t.Fatalf("a prophet reader without read on the app sees no cost, got %v %v", hidden, err)
	}

	if _, err := disciple.NewDelete().Model(&shared.AppCost{ID: correction.ID}).Access(ac).Exec(ctx); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := prophet.NewRaw(`SELECT count(*) FROM claimius.app_cost WHERE id = ? AND sa_deleted_at IS NULL`, correction.ID).Scan(ctx, &live); err != nil || live != 0 {
		t.Fatalf("a routed delete soft deletes the row on the prophet, got %d %v", live, err)
	}
	revived, err := shared.GetAppCost(ctx, prophet, owner, app.ID, shared.AppCostQuery{Module: app.Slug, Granularity: "day"})
	if err != nil || len(revived) != 1 || revived[0].Quantity != 30 {
		t.Fatalf("deleting a correction brings the superseded row back into the sums, got %v %v", revived, err)
	}
}

func TestProphetBaselineRealignsAnAlpha0109CostTable(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	baseline, err := os.ReadFile("../database/V1.1__claimius_baseline.sql")
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	err = psdb.Instance().RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		for _, statement := range []string{
			`DELETE FROM claimius.app_cost`,
			`ALTER TABLE claimius.app_cost DROP COLUMN period_end, DROP COLUMN kind, DROP COLUMN unit_price, DROP COLUMN ref, DROP COLUMN supersedes_id, DROP COLUMN status, DROP COLUMN schema`,
			`ALTER TABLE claimius.app_cost ADD CHECK (date_trunc('hour', period_start AT TIME ZONE 'UTC') = period_start AT TIME ZONE 'UTC')`,
			`CREATE UNIQUE INDEX idx_app_cost_app_module_period ON claimius.app_cost (app_id, module, period_start) WHERE sa_deleted_at IS NULL`,
			`INSERT INTO claimius.app_cost (app_id, module, period_start, quantity, unit, amount, currency, sa_owner_id, sa_created_by)
			 SELECT id, 'old', '2026-10-30 09:00+00', 10, 'request', 1.5, 'NOK', sa_owner_id, sa_created_by FROM claimius.samna_app WHERE sa_deleted_at IS NULL LIMIT 1`,
			string(baseline),
			string(baseline),
		} {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		var old shared.AppCost
		if err := tx.NewSelect().Model(&old).Where("module = 'old'").Scan(ctx); err != nil {
			return err
		}
		if old.Kind != "usage" || !old.PeriodEnd.Equal(old.PeriodStart.Add(time.Hour)) || old.Ref != "2026-10-30T09:00:00Z" || old.Status != "settled" || string(old.Schema) != `{}` {
			t.Fatalf("the realign backfills an alpha0109 row, got %+v", old)
		}
		var indexes []string
		if err := tx.NewRaw(`SELECT indexname FROM pg_indexes WHERE schemaname = 'claimius' AND tablename = 'app_cost' ORDER BY 1`).Scan(ctx, &indexes); err != nil {
			return err
		}
		if strings.Join(indexes, ",") != "app_cost_pkey,idx_app_cost_app_module_ref,idx_app_cost_app_period,idx_app_cost_supersedes" {
			t.Fatalf("the realign swaps the unique index, got %v", indexes)
		}
		return errCostRollback
	})
	if !errors.Is(err, errCostRollback) {
		t.Fatalf("realign: %v", err)
	}
}
