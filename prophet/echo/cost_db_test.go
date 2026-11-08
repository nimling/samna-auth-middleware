//go:build dbtest

package echo_test

import (
	"context"
	"errors"
	"net/http/httptest"
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
)

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
	hour := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	first := &shared.AppCost{Module: app.Slug, PeriodStart: hour, Quantity: 10, Unit: "request", Amount: 1.5, Currency: "NOK"}
	if err := dsdb.PutAppCost(ctx, disciple, ac, first); err != nil {
		t.Fatalf("the app's actor reports an hour through the prophet, got %v", err)
	}
	var stored struct {
		AppID     uuid.UUID  `bun:"app_id"`
		OwnerID   uuid.UUID  `bun:"sa_owner_id"`
		CreatedBy *uuid.UUID `bun:"sa_created_by"`
		Quantity  float64    `bun:"quantity"`
	}
	if err := prophet.NewRaw(`SELECT app_id, sa_owner_id, sa_created_by, quantity FROM claimius.app_cost WHERE id = ?`, first.ID).Scan(ctx, &stored); err != nil {
		t.Fatalf("read the stored hour: %v", err)
	}
	if stored.AppID != app.ID || stored.OwnerID != app.Org || stored.CreatedBy == nil || *stored.CreatedBy != ownerGrant {
		t.Fatalf("the prophet stamps the calling app, its organization and the actor's grant, got %+v", stored)
	}
	var local int
	if err := disciple.NewRaw(`SELECT count(*) FROM claimius.app_cost WHERE id = ?`, first.ID).Scan(ctx, &local); err != nil || local != 1 {
		t.Fatalf("the prophet's answer is copied onto the disciple, got %d %v", local, err)
	}

	again := &shared.AppCost{Module: app.Slug, PeriodStart: hour, Quantity: 25, Unit: "request", Amount: 3, Currency: "NOK"}
	if err := dsdb.PutAppCost(ctx, disciple, ac, again); err != nil {
		t.Fatalf("report the same hour again: %v", err)
	}
	var live int
	if err := prophet.NewRaw(`SELECT count(*) FROM claimius.app_cost WHERE app_id = ? AND module = ? AND sa_deleted_at IS NULL`, app.ID, app.Slug).Scan(ctx, &live); err != nil || live != 1 || again.ID != first.ID || again.Quantity != 25 {
		t.Fatalf("an hour reported twice is one row updated, got %d rows id %s quantity %v %v", live, again.ID, again.Quantity, err)
	}
	if err := dsdb.PutAppCost(ctx, disciple, ac, &shared.AppCost{Module: app.Slug, PeriodStart: hour.Add(time.Hour), Quantity: 5, Unit: "request", Amount: 0.5, Currency: "NOK"}); err != nil {
		t.Fatalf("report the next hour: %v", err)
	}
	if err := dsdb.PutAppCost(ctx, disciple, ac, &shared.AppCost{Module: "uptime", PeriodStart: hour, Quantity: 60, Unit: "check", Amount: 0.06, Currency: "NOK"}); err != nil {
		t.Fatalf("report a built in module's hour: %v", err)
	}

	forged := dsdb.PutAppCost(ctx, disciple, &shared.AccessClaim{UserID: outsider, OwnerID: app.Org, UserClaimID: &ownerGrant},
		&shared.AppCost{Module: app.Slug, PeriodStart: hour, Quantity: 1, Unit: "request", Currency: "NOK"})
	if !errors.Is(forged, shared.ErrNoAccess) {
		t.Fatalf("a write stamped with a grant the actor does not hold is refused by the prophet, got %v", forged)
	}

	if _, err := replica.PullSync(ctx); err != nil {
		t.Fatalf("pull: %v", err)
	}
	hours, err := shared.GetAppCost(ctx, disciple, owner, uuid.Nil, "", time.Time{}, time.Time{}, "hour")
	if err != nil || len(hours) != 3 {
		t.Fatalf("the hours reach the disciple and its reader, got %v %v", hours, err)
	}
	days, err := shared.GetAppCost(ctx, disciple, owner, uuid.Nil, app.Slug, time.Time{}, time.Time{}, "day")
	if err != nil || len(days) != 1 || days[0].Quantity != 30 || days[0].Hours != 2 {
		t.Fatalf("a day bucket on the disciple folds the app's hours, got %v %v", days, err)
	}
	if hidden, err := shared.GetAppCost(ctx, disciple, outsider, uuid.Nil, "", time.Time{}, time.Time{}, "hour"); err != nil || len(hidden) != 0 {
		t.Fatalf("a reader without read on the organization sees no cost, got %v %v", hidden, err)
	}
	onProphet, err := shared.GetAppCost(ctx, prophet, owner, app.ID, "", time.Time{}, time.Time{}, "day")
	if err != nil || len(onProphet) != 2 || onProphet[0].AppID != app.ID {
		t.Fatalf("the prophet buckets the app's cost for a reader with read on the app, got %v %v", onProphet, err)
	}
	if hidden, err := shared.GetAppCost(ctx, prophet, outsider, app.ID, "", time.Time{}, time.Time{}, "hour"); err != nil || len(hidden) != 0 {
		t.Fatalf("a prophet reader without read on the app sees no cost, got %v %v", hidden, err)
	}

	if _, err := disciple.NewDelete().Model(&shared.AppCost{ID: first.ID}).Access(ac).Exec(ctx); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := prophet.NewRaw(`SELECT count(*) FROM claimius.app_cost WHERE id = ? AND sa_deleted_at IS NULL`, first.ID).Scan(ctx, &live); err != nil || live != 0 {
		t.Fatalf("a routed delete soft deletes the hour on the prophet, got %d %v", live, err)
	}
}
