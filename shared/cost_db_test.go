//go:build dbtest

package shared_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	shared "github.com/nimling/samna-auth-middleware/v2/shared"
)

func TestPutAppCostUpsertsTheHourAndGetAppCostBucketsIt(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	db.RouteToProphet = false
	ac, err := db.MutationAccess(ctx, userA, orgRoot, shared.OBJECT_TYPE_ORGANIZATION, shared.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	module := "gotest_cost_" + uuid.NewString()[:8]
	hour := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)

	first := &shared.AppCost{Module: module, PeriodStart: hour.Add(17 * time.Minute), Quantity: 10, Unit: "request", Amount: 1.5, Currency: "NOK"}
	if err := sdb.PutAppCost(ctx, db, ac, first); err != nil {
		t.Fatalf("put the hour: %v", err)
	}
	if first.ID == uuid.Nil || !first.PeriodStart.Equal(hour) || first.OwnerID != orgRoot || first.CreatedBy == nil || *first.CreatedBy != *ac.UserClaimID {
		t.Fatalf("the hour must be stored truncated under the actor's owner and grant, got %+v", first)
	}
	again := &shared.AppCost{Module: module, PeriodStart: hour, Quantity: 25, Unit: "request", Amount: 3, Currency: "NOK", Detail: []byte(`{"endpoints": 4}`)}
	if err := sdb.PutAppCost(ctx, db, ac, again); err != nil {
		t.Fatalf("put the hour again: %v", err)
	}
	if again.ID != first.ID || again.Quantity != 25 || again.Amount != 3 {
		t.Fatalf("the second write of one hour must update the same row, got %+v", again)
	}
	var live int
	if err := db.NewRaw(`SELECT count(*) FROM claimius.app_cost WHERE module = ? AND sa_deleted_at IS NULL`, module).Scan(ctx, &live); err != nil || live != 1 {
		t.Fatalf("one live row per hour, got %d %v", live, err)
	}
	next := &shared.AppCost{Module: module, PeriodStart: hour.Add(time.Hour), Quantity: 5, Unit: "request", Amount: 0.5, Currency: "NOK"}
	if err := sdb.PutAppCost(ctx, db, ac, next); err != nil {
		t.Fatalf("put the next hour: %v", err)
	}

	hours, err := shared.GetAppCost(ctx, db, userA, uuid.Nil, module, time.Time{}, time.Time{}, "hour")
	if err != nil || len(hours) != 2 {
		t.Fatalf("a reader with read on the organization gets one bucket per hour, got %v %v", hours, err)
	}
	if hours[0].Quantity != 25 || hours[0].Hours != 1 || hours[1].Quantity != 5 || !hours[0].PeriodStart.Equal(hour) {
		t.Fatalf("the hour buckets carry the summed quantity and the hour count, got %+v", hours)
	}
	days, err := shared.GetAppCost(ctx, db, userA, uuid.Nil, module, time.Time{}, time.Time{}, "day")
	if err != nil || len(days) != 1 || days[0].Quantity != 30 || days[0].Amount != 3.5 || days[0].Hours != 2 || days[0].Currency != "NOK" {
		t.Fatalf("a day bucket sums the hours it folds, got %v %v", days, err)
	}
	if !days[0].PeriodStart.Equal(time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("a day bucket starts at the UTC midnight, got %s", days[0].PeriodStart)
	}
	window, err := shared.GetAppCost(ctx, db, userA, uuid.Nil, module, hour.Add(time.Hour), hour.Add(2*time.Hour), "hour")
	if err != nil || len(window) != 1 || window[0].Quantity != 5 {
		t.Fatalf("from is inclusive and to exclusive on the hour, got %v %v", window, err)
	}
	none, err := shared.GetAppCost(ctx, db, userB, uuid.Nil, module, time.Time{}, time.Time{}, "hour")
	if err != nil || len(none) != 0 {
		t.Fatalf("a reader without read on the organization gets no bucket, got %v %v", none, err)
	}
	if _, err := shared.GetAppCost(ctx, db, userA, uuid.Nil, module, time.Time{}, time.Time{}, "week"); err == nil {
		t.Fatal("a granularity other than hour or day must be refused")
	}

	if _, err := db.NewDelete().Model(&shared.AppCost{ID: next.ID}).Access(ac).Exec(ctx); err != nil {
		t.Fatalf("delete: %v", err)
	}
	after, err := shared.GetAppCost(ctx, db, userA, uuid.Nil, module, time.Time{}, time.Time{}, "hour")
	if err != nil || len(after) != 1 {
		t.Fatalf("a deleted hour leaves the buckets, got %v %v", after, err)
	}
	if err := sdb.PutAppCost(ctx, db, nil, &shared.AppCost{Module: module, PeriodStart: hour, Unit: "request", Currency: "NOK"}); !errors.Is(err, shared.ErrNoAccess) {
		t.Fatalf("a write without an actor is ErrNoAccess, got %v", err)
	}
}
