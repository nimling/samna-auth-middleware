//go:build dbtest

package shared_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	shared "github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/uptrace/bun"
)

var errRollback = errors.New("rollback")

func costModule() string { return "gotest_cost_" + uuid.NewString()[:8] }

func at(t *testing.T, value string) *time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("time %s: %v", value, err)
	}
	return &parsed
}

func number(v float64) *float64 { return &v }

func costActor(t *testing.T, db *shared.DB) *shared.AccessClaim {
	t.Helper()
	db.RouteToProphet = false
	ac, err := db.MutationAccess(context.Background(), userA, orgRoot, shared.OBJECT_TYPE_ORGANIZATION, shared.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	return ac
}

func TestPutAppCostAcceptsTheThreeFormsAndReplacesByRef(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	ac := costActor(t, db)
	module := costModule()

	row, err := sdb.PutAppCost(ctx, db, ac, &shared.AppCostInput{Module: module, Kind: "usage", PeriodStart: at(t, "2026-10-30T09:17:00Z"), PeriodEnd: at(t, "2026-10-30T10:17:00Z"),
		Quantity: number(10), Unit: "request", UnitPrice: number(0.15), Currency: "NOK", Ref: "r1"})
	if err != nil {
		t.Fatalf("row form: %v", err)
	}
	if row.ID == uuid.Nil || row.Kind != "usage" || row.Amount != 1.5 || row.Status != "settled" || !row.PeriodStart.Equal(*at(t, "2026-10-30T09:17:00Z")) ||
		string(row.Schema) != `{}` || string(row.Detail) != `{}` || row.OwnerID != orgRoot || row.CreatedBy == nil || *row.CreatedBy != *ac.UserClaimID {
		t.Fatalf("the row form computes the amount, keeps the window as given and leaves schema and detail empty, got %+v", row)
	}
	again, err := sdb.PutAppCost(ctx, db, ac, &shared.AppCostInput{Module: module, Kind: "usage", PeriodStart: at(t, "2026-10-30T09:17:00Z"), PeriodEnd: at(t, "2026-10-30T10:17:00Z"),
		Quantity: number(25), Unit: "request", UnitPrice: number(0.15), Amount: number(3.75), Currency: "NOK", Ref: "r1", Status: "estimated"})
	if err != nil {
		t.Fatalf("replace by ref: %v", err)
	}
	var live int
	if err := db.NewRaw(`SELECT count(*) FROM claimius.app_cost WHERE module = ? AND sa_deleted_at IS NULL`, module).Scan(ctx, &live); err != nil || live != 1 || again.ID != row.ID || again.Quantity != 25 || again.Status != "estimated" {
		t.Fatalf("a rerun of the same ref replaces the row's values, got %d rows %+v %v", live, again, err)
	}

	items, err := sdb.PutAppCost(ctx, db, ac, &shared.AppCostInput{Module: module, PeriodStart: at(t, "2026-10-30T11:00:00Z"), PeriodEnd: at(t, "2026-10-30T12:00:00Z"), Currency: "NOK", Ref: "r2",
		Items:  []shared.AppCostItem{{Description: "mails", Quantity: 2, Unit: "mail", UnitPrice: number(0.5)}, {Description: "more mails", Quantity: 3, Unit: "mail", Amount: number(1.5)}},
		Detail: json.RawMessage(`{"subject": {"type": "claimius.organization", "id": "` + orgRoot.String() + `", "name": "Root"}}`)})
	if err != nil {
		t.Fatalf("items form: %v", err)
	}
	var detail struct {
		Subject struct {
			ID string `json:"id"`
		} `json:"subject"`
		Items []shared.AppCostItem `json:"items"`
	}
	if err := json.Unmarshal(items.Detail, &detail); err != nil {
		t.Fatalf("detail: %v", err)
	}
	if items.Kind != "usage" || items.Quantity != 5 || items.Unit != "mail" || items.Amount != 2.5 || len(detail.Items) != 2 || detail.Items[0].Amount == nil || *detail.Items[0].Amount != 1 ||
		detail.Subject.ID != orgRoot.String() || !strings.Contains(string(items.Schema), shared.COST_SCHEMA_ID) {
		t.Fatalf("the items form sums the items into the row, fills their amounts and implies the default schema, got %+v %s", items, items.Detail)
	}
	mixed, err := sdb.PutAppCost(ctx, db, ac, &shared.AppCostInput{Module: module, Kind: "period", PeriodStart: at(t, "2026-10-01T00:00:00Z"), PeriodEnd: at(t, "2026-11-01T00:00:00Z"), Currency: "NOK", Ref: "r3",
		Items: []shared.AppCostItem{{Description: "seat", Quantity: 1, Unit: "seat", Amount: number(300)}, {Description: "storage", Quantity: 20, Unit: "gb", Amount: number(10)}}})
	if err != nil || mixed.Quantity != 2 || mixed.Unit != "item" || mixed.Amount != 310 || mixed.Kind != "period" {
		t.Fatalf("items of different units count as items, got %+v %v", mixed, err)
	}

	event, err := sdb.PutAppCost(ctx, db, ac, &shared.AppCostInput{Module: module, At: at(t, "2026-10-30T13:45:00Z"), Amount: number(99), Currency: "NOK", Ref: "e1", Detail: json.RawMessage(`{"invoice": 7}`)})
	if err != nil {
		t.Fatalf("event form: %v", err)
	}
	if event.Kind != "fee" || !event.PeriodStart.Equal(*at(t, "2026-10-30T13:45:00Z")) || !event.PeriodEnd.Equal(event.PeriodStart) || event.Quantity != 1 || event.Unit != "event" || event.Amount != 99 || string(event.Schema) != `{}` {
		t.Fatalf("the event form is a fee at an instant with an empty schema, got %+v", event)
	}

	hours, err := shared.GetAppCost(ctx, db, userA, uuid.Nil, shared.AppCostQuery{Module: module, From: *at(t, "2026-10-30T09:00:00Z"), To: *at(t, "2026-10-30T14:00:00Z")})
	if err != nil || len(hours) != 9 {
		t.Fatalf("a reader with read on the organization gets the buckets, got %d %v %v", len(hours), hours, err)
	}
	if hours[0].Kind != "period" || hours[0].Rows != 1 || hours[0].Hours != 1 {
		t.Fatalf("hour buckets carry the kind, the row count and the hours folded, got %+v", hours[0])
	}
	days, err := shared.GetAppCost(ctx, db, userA, uuid.Nil, shared.AppCostQuery{Module: module, Granularity: "day", From: *at(t, "2026-10-30T00:00:00Z"), To: *at(t, "2026-10-31T00:00:00Z")})
	if err != nil || len(days) != 4 {
		t.Fatalf("a day window buckets each kind and unit, got %v %v", days, err)
	}
	for _, bucket := range days {
		switch {
		case bucket.Kind == "fee" && (bucket.Amount != 99 || bucket.Hours != 0):
			t.Fatalf("a fee falls whole into the bucket of its instant, got %+v", bucket)
		case bucket.Kind == "period" && (bucket.Quantity < 0.064 || bucket.Quantity > 0.065 || bucket.Amount != 10 || bucket.Hours != 24):
			t.Fatalf("a period row is prorated by the day's share of its month, got %+v", bucket)
		case bucket.Kind == "usage" && bucket.Unit == "mail" && (bucket.Quantity != 5 || bucket.SchemaID == nil || *bucket.SchemaID != shared.COST_SCHEMA_ID || string(bucket.GroupKey) != `{"subject.id": "`+orgRoot.String()+`"}`):
			t.Fatalf("the default schema groups by subject and names itself, got %+v %s", bucket, bucket.GroupKey)
		}
	}
	estimated, err := shared.GetAppCost(ctx, db, userA, uuid.Nil, shared.AppCostQuery{Module: module, Status: "estimated", Granularity: "day"})
	if err != nil || len(estimated) != 1 || estimated[0].Quantity != 25 {
		t.Fatalf("status narrows the rows, got %v %v", estimated, err)
	}
	subject, err := shared.GetAppCost(ctx, db, userA, uuid.Nil, shared.AppCostQuery{Module: module, SubjectType: "claimius.organization", SubjectID: orgRoot})
	if err != nil || len(subject) != 1 || subject[0].Unit != "mail" {
		t.Fatalf("the subject narrows the rows through detail.subject, got %v %v", subject, err)
	}
	if none, err := shared.GetAppCost(ctx, db, userB, uuid.Nil, shared.AppCostQuery{Module: module}); err != nil || len(none) != 0 {
		t.Fatalf("a reader without read on the organization gets no bucket, got %v %v", none, err)
	}
	if _, err := shared.GetAppCost(ctx, db, userA, uuid.Nil, shared.AppCostQuery{Module: module, Granularity: "week"}); err == nil {
		t.Fatal("a granularity other than hour, day or month must be refused")
	}

	if _, err := db.NewDelete().Model(&shared.AppCost{ID: event.ID}).Access(ac).Exec(ctx); err != nil {
		t.Fatalf("delete: %v", err)
	}
	after, err := shared.GetAppCost(ctx, db, userA, uuid.Nil, shared.AppCostQuery{Module: module, From: *at(t, "2026-10-30T09:00:00Z"), To: *at(t, "2026-10-30T14:00:00Z")})
	if err != nil || len(after) != 8 {
		t.Fatalf("a deleted row leaves the buckets, got %v %v", after, err)
	}
	if _, err := sdb.PutAppCost(ctx, db, nil, &shared.AppCostInput{Module: module, At: at(t, "2026-10-30T13:45:00Z"), Amount: number(1), Currency: "NOK", Ref: "e2"}); !errors.Is(err, shared.ErrNoAccess) {
		t.Fatalf("a write without an actor is ErrNoAccess, got %v", err)
	}
}

func TestGetAppCostProratesAcrossDaysAndLeavesSupersededRowsOut(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	ac := costActor(t, db)
	module := costModule()

	span, err := sdb.PutAppCost(ctx, db, ac, &shared.AppCostInput{Module: module, Kind: "usage", PeriodStart: at(t, "2026-10-30T22:00:00Z"), PeriodEnd: at(t, "2026-10-31T02:00:00Z"),
		Quantity: number(40), Unit: "request", Amount: number(4), Currency: "NOK", Ref: "span"})
	if err != nil {
		t.Fatalf("span: %v", err)
	}
	if _, err := sdb.PutAppCost(ctx, db, ac, &shared.AppCostInput{Module: module, At: at(t, "2026-10-30T23:30:00Z"), Amount: number(100), Currency: "NOK", Ref: "fee"}); err != nil {
		t.Fatalf("fee: %v", err)
	}
	days, err := shared.GetAppCost(ctx, db, userA, uuid.Nil, shared.AppCostQuery{Module: module, Granularity: "day"})
	if err != nil || len(days) != 3 {
		t.Fatalf("a usage row across midnight lands in both days beside the fee, got %v %v", days, err)
	}
	if days[0].Kind != "fee" || !days[0].PeriodStart.Equal(*at(t, "2026-10-30T00:00:00Z")) || days[1].Quantity != 20 || days[1].Amount != 2 || days[1].Hours != 2 || days[2].Quantity != 20 || !days[2].PeriodStart.Equal(*at(t, "2026-10-31T00:00:00Z")) {
		t.Fatalf("the overlap prorates quantity, amount and hours into each day, got %+v", days)
	}

	if _, err := sdb.PutAppCost(ctx, db, ac, &shared.AppCostInput{Module: module, Kind: "usage", PeriodStart: at(t, "2026-10-30T22:00:00Z"), PeriodEnd: at(t, "2026-10-31T02:00:00Z"),
		Quantity: number(1), Unit: "request", Amount: number(1), Currency: "NOK", Ref: "span", SupersedesID: &span.ID}); !errors.Is(err, shared.ErrBadAppCost) {
		t.Fatalf("a row cannot supersede its own ref, got %v", err)
	}
	missing := uuid.New()
	if _, err := sdb.PutAppCost(ctx, db, ac, &shared.AppCostInput{Module: module, Kind: "usage", PeriodStart: at(t, "2026-10-30T22:00:00Z"), PeriodEnd: at(t, "2026-10-31T02:00:00Z"),
		Quantity: number(1), Unit: "request", Amount: number(1), Currency: "NOK", Ref: "span-2", SupersedesID: &missing}); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("supersedes_id must name a live row, got %v", err)
	}
	correction, err := sdb.PutAppCost(ctx, db, ac, &shared.AppCostInput{Module: module, Kind: "usage", PeriodStart: at(t, "2026-10-30T22:00:00Z"), PeriodEnd: at(t, "2026-10-31T02:00:00Z"),
		Quantity: number(80), Unit: "request", Amount: number(8), Currency: "NOK", Ref: "span-2", SupersedesID: &span.ID})
	if err != nil || correction.SupersedesID == nil || *correction.SupersedesID != span.ID {
		t.Fatalf("a correction names the row it supersedes, got %+v %v", correction, err)
	}
	var kept int
	if err := db.NewRaw(`SELECT count(*) FROM claimius.app_cost WHERE id = ? AND sa_deleted_at IS NULL`, span.ID).Scan(ctx, &kept); err != nil || kept != 1 {
		t.Fatalf("the superseded row stays, got %d %v", kept, err)
	}
	hours, err := shared.GetAppCost(ctx, db, userA, uuid.Nil, shared.AppCostQuery{Module: module, From: *at(t, "2026-10-31T00:00:00Z")})
	if err != nil || len(hours) != 2 || hours[0].Quantity != 20 || hours[0].Amount != 2 || hours[0].Rows != 1 {
		t.Fatalf("the window keeps the hours from midnight and the sums come from the correction alone, got %v %v", hours, err)
	}
	month, err := shared.GetAppCost(ctx, db, userA, uuid.Nil, shared.AppCostQuery{Module: module, Granularity: "month"})
	if err != nil || len(month) != 2 {
		t.Fatalf("a month bucket per kind, got %v %v", month, err)
	}
	for _, bucket := range month {
		if bucket.Kind == "usage" && (bucket.Quantity != 80 || bucket.Hours != 4 || !bucket.PeriodStart.Equal(*at(t, "2026-10-01T00:00:00Z"))) {
			t.Fatalf("the month folds the whole corrected window, got %+v", bucket)
		}
	}
}

func TestGetAppCostAccumulatesByTheRowsSchema(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	ac := costActor(t, db)
	module := costModule()
	for _, method := range []string{"sum", "max", "last", "count"} {
		schema := json.RawMessage(`{"$id": "https://samna.io/schema/test/` + method + `", "type": "object", "x-samna": {"title": "Gauge", "accumulate": {"method": "` + method + `", "group": ["subject.id"]}}}`)
		for n := 1; n <= 4; n++ {
			subject := "a"
			if n%2 == 1 {
				subject = "b"
			}
			start := at(t, "2026-10-30T09:00:00Z").Add(time.Duration(n) * time.Minute)
			end := start.Add(time.Minute)
			if _, err := sdb.PutAppCost(ctx, db, ac, &shared.AppCostInput{Module: module + "_" + method, Kind: "usage", PeriodStart: &start, PeriodEnd: &end, Quantity: number(float64(n)), Unit: "device",
				Amount: number(float64(n) / 10), Currency: "NOK", Ref: method + "-" + subject + "-" + uuid.NewString()[:4], Schema: schema, Detail: json.RawMessage(`{"subject": {"id": "` + subject + `"}}`)}); err != nil {
				t.Fatalf("%s row %d: %v", method, n, err)
			}
		}
	}
	start, err := sdb.PutAppCost(ctx, db, ac, &shared.AppCostInput{Module: module + "_start", Kind: "usage", PeriodStart: at(t, "2026-10-30T23:00:00Z"), PeriodEnd: at(t, "2026-10-31T03:00:00Z"), Quantity: number(4), Unit: "h",
		Amount: number(4), Currency: "NOK", Ref: "start", Schema: json.RawMessage(`{"x-samna": {"accumulate": {"proration": "start"}}}`)})
	if err != nil {
		t.Fatalf("start proration row: %v", err)
	}
	want := map[string][2]float64{"sum": {6, 4}, "max": {4, 3}, "last": {4, 3}, "count": {2, 2}}
	for method, expected := range want {
		buckets, err := shared.GetAppCost(ctx, db, userA, uuid.Nil, shared.AppCostQuery{Module: module + "_" + method})
		if err != nil || len(buckets) != 2 {
			t.Fatalf("%s: one bucket per group key, got %v %v", method, buckets, err)
		}
		for _, bucket := range buckets {
			var key struct {
				Subject string `json:"subject.id"`
			}
			if err := json.Unmarshal(bucket.GroupKey, &key); err != nil || bucket.SchemaID == nil || !strings.HasSuffix(*bucket.SchemaID, method) || bucket.Rows != 2 {
				t.Fatalf("%s: the bucket names its schema, its group key and its row count, got %+v %s %v", method, bucket, bucket.GroupKey, err)
			}
			got := expected[0]
			if key.Subject == "b" {
				got = expected[1]
			}
			if bucket.Quantity != got {
				t.Fatalf("%s for subject %s: want %v, got %+v", method, key.Subject, got, bucket)
			}
		}
	}
	anchored, err := shared.GetAppCost(ctx, db, userA, uuid.Nil, shared.AppCostQuery{Module: module + "_start", Granularity: "day"})
	if err != nil || len(anchored) != 1 || !anchored[0].PeriodStart.Equal(*at(t, "2026-10-30T00:00:00Z")) || anchored[0].Quantity != 4 || anchored[0].Hours != 4 || anchored[0].SchemaID != nil || anchored[0].GroupKey != nil {
		t.Fatalf("proration start lands the whole row in the bucket of its start, with no schema id and no group key, got %v %v %+v", anchored, err, start)
	}
}

func TestPutAppCostRejectsWhatTheFormsAndTheSchemaForbid(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	ac := costActor(t, db)
	module := costModule()
	window := func(input shared.AppCostInput) *shared.AppCostInput {
		input.Module = module
		if input.Currency == "" {
			input.Currency = "NOK"
		}
		if input.Ref == "" {
			input.Ref = uuid.NewString()
		}
		if input.At == nil && input.PeriodStart == nil {
			input.PeriodStart = at(t, "2026-10-30T09:00:00Z")
			input.PeriodEnd = at(t, "2026-10-30T10:00:00Z")
		}
		return &input
	}
	usage := func(input shared.AppCostInput) *shared.AppCostInput {
		if input.Kind == "" {
			input.Kind = "usage"
		}
		if input.Quantity == nil {
			input.Quantity = number(1)
		}
		if input.Unit == "" {
			input.Unit = "x"
		}
		if input.Amount == nil && input.UnitPrice == nil {
			input.Amount = number(1)
		}
		return window(input)
	}
	cases := []struct {
		name  string
		input *shared.AppCostInput
		want  string
	}{
		{"ref required", window(shared.AppCostInput{Kind: "usage", Quantity: number(1), Unit: "x", Amount: number(1), Ref: " "}), "ref required"},
		{"currency", usage(shared.AppCostInput{Currency: "nok"}), "ISO 4217"},
		{"kind", window(shared.AppCostInput{Quantity: number(1), Unit: "x", Amount: number(1)}), "kind required"},
		{"bad kind", usage(shared.AppCostInput{Kind: "tax"}), "kind must be one of"},
		{"bad status", usage(shared.AppCostInput{Status: "paid"}), "status must be one of"},
		{"amount or price", window(shared.AppCostInput{Kind: "usage", Quantity: number(1), Unit: "x"}), "amount or unit_price required"},
		{"amount mismatch", usage(shared.AppCostInput{Quantity: number(2), UnitPrice: number(0.5), Amount: number(2)}), "amount must equal quantity times unit_price"},
		{"fee window", usage(shared.AppCostInput{Kind: "fee"}), "a fee has period_end equal to period_start"},
		{"end before start", &shared.AppCostInput{Module: module, Kind: "usage", PeriodStart: at(t, "2026-10-30T10:00:00Z"), PeriodEnd: at(t, "2026-10-30T09:00:00Z"), Quantity: number(1), Unit: "x", Amount: number(1), Currency: "NOK", Ref: "x"}, "period_end must not be before period_start"},
		{"negative usage", usage(shared.AppCostInput{Amount: number(-1)}), "amount must not be negative"},
		{"event with window", &shared.AppCostInput{Module: module, At: at(t, "2026-10-30T10:00:00Z"), PeriodStart: at(t, "2026-10-30T10:00:00Z"), Amount: number(1), Currency: "NOK", Ref: "x"}, "period_start not allowed with at"},
		{"event without amount", &shared.AppCostInput{Module: module, At: at(t, "2026-10-30T10:00:00Z"), Currency: "NOK", Ref: "x"}, "amount required with at"},
		{"event kind", &shared.AppCostInput{Module: module, At: at(t, "2026-10-30T10:00:00Z"), Kind: "usage", Amount: number(1), Currency: "NOK", Ref: "x"}, "kind must be fee with at"},
		{"items with quantity", window(shared.AppCostInput{Quantity: number(1), Items: []shared.AppCostItem{{Description: "a", Quantity: 1, Unit: "x", Amount: number(1)}}}), "quantity not allowed with items"},
		{"item without price", window(shared.AppCostInput{Items: []shared.AppCostItem{{Description: "a", Quantity: 1, Unit: "x"}}}), "items[0] needs unit_price or amount"},
		{"item without description", window(shared.AppCostInput{Items: []shared.AppCostItem{{Quantity: 1, Unit: "x", Amount: number(1)}}}), "items[0].description required"},
		{"items in detail", window(shared.AppCostInput{Items: []shared.AppCostItem{{Description: "a", Quantity: 1, Unit: "x", Amount: number(1)}}, Detail: json.RawMessage(`{"items": []}`)}), "detail.items not allowed with items"},
		{"detail not an object", usage(shared.AppCostInput{Detail: json.RawMessage(`[1]`)}), "detail must be a JSON object"},
		{"schema not an object", usage(shared.AppCostInput{Schema: json.RawMessage(`[]`)}), "schema must be a JSON object"},
		{"schema mismatch names the path", usage(shared.AppCostInput{Schema: json.RawMessage(`{"type": "object", "properties": {"device": {"type": "object", "required": ["serial"]}}}`), Detail: json.RawMessage(`{"device": {}}`)}), "detail at '/device': missing property 'serial'"},
		{"default schema rejects unknown keys", window(shared.AppCostInput{Items: []shared.AppCostItem{{Description: "a", Quantity: 1, Unit: "x", Amount: number(1)}}, Detail: json.RawMessage(`{"invoice": 7}`)}), "detail at '': additional properties 'invoice' not allowed"},
		{"default schema checks the subject", window(shared.AppCostInput{Items: []shared.AppCostItem{{Description: "a", Quantity: 1, Unit: "x", Amount: number(1)}}, Detail: json.RawMessage(`{"subject": {"type": "device", "id": "7"}}`)}), "detail at '/subject/"},
		{"default schema sums the items", usage(shared.AppCostInput{Amount: number(5), Schema: shared.CostSchema, Detail: json.RawMessage(`{"items": [{"description": "a", "quantity": 2, "unit": "x", "unit_price": 1}]}`)}), "detail.items amounts sum to 2, amount is 5"},
		{"x-samna not an object", usage(shared.AppCostInput{Schema: json.RawMessage(`{"x-samna": 1}`)}), "schema x-samna must be an object"},
		{"accumulate method", usage(shared.AppCostInput{Schema: json.RawMessage(`{"x-samna": {"accumulate": {"method": "avg"}}}`)}), "x-samna.accumulate.method must be one of sum, max, last, count"},
		{"accumulate proration", usage(shared.AppCostInput{Schema: json.RawMessage(`{"x-samna": {"accumulate": {"proration": "end"}}}`)}), "x-samna.accumulate.proration must be one of overlap, start"},
		{"accumulate group", usage(shared.AppCostInput{Schema: json.RawMessage(`{"x-samna": {"accumulate": {"group": "subject.id"}}}`)}), "x-samna.accumulate.group must be an array of paths"},
	}
	for _, c := range cases {
		_, err := sdb.PutAppCost(ctx, db, ac, c.input)
		if !errors.Is(err, shared.ErrBadAppCost) || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: want %q, got %v", c.name, c.want, err)
		}
	}
	free, err := sdb.PutAppCost(ctx, db, ac, usage(shared.AppCostInput{Detail: json.RawMessage(`{"anything": [1, {"goes": true}], "invoice": 7}`)}))
	if err != nil || string(free.Schema) != `{}` || !strings.Contains(string(free.Detail), `"goes":true`) {
		t.Fatalf("with an empty schema detail is any object, got %+v %v", free, err)
	}
	hinted, err := sdb.PutAppCost(ctx, db, ac, usage(shared.AppCostInput{Schema: json.RawMessage(`{"$id": "https://samna.io/schema/device/1", "type": "object", "required": ["serial"], "x-samna": {"title": "Device", "columns": ["serial"], "unit_label": "Device hours"}}`), Detail: json.RawMessage(`{"serial": "A1"}`)}))
	if err != nil || !strings.Contains(string(hinted.Schema), `"x-samna":{"title":"Device"`) {
		t.Fatalf("a schema with presentation hints is kept compact on the row, got %+v %v", hinted, err)
	}
	if _, err := sdb.PutAppCost(ctx, db, ac, usage(shared.AppCostInput{Schema: json.RawMessage(`{"$id": "https://samna.io/schema/device/1", "type": "object", "required": ["serial"]}`), Detail: json.RawMessage(`{}`)})); !errors.Is(err, shared.ErrBadAppCost) || !strings.Contains(err.Error(), "missing property 'serial'") {
		t.Fatalf("a row schema is enforced on detail, got %v", err)
	}
}

func TestBaselineRealignsAnAlpha0109CostTable(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	baseline, err := os.ReadFile("../disciple/database/V1.1__claimius_baseline.sql")
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	err = db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		for _, statement := range []string{
			`DELETE FROM claimius.app_cost`,
			`ALTER TABLE claimius.app_cost DROP COLUMN period_end, DROP COLUMN kind, DROP COLUMN unit_price, DROP COLUMN ref, DROP COLUMN supersedes_id, DROP COLUMN status, DROP COLUMN schema`,
			`ALTER TABLE claimius.app_cost ADD CHECK (date_trunc('hour', period_start AT TIME ZONE 'UTC') = period_start AT TIME ZONE 'UTC')`,
			`CREATE UNIQUE INDEX idx_app_cost_module_period ON claimius.app_cost (module, period_start) WHERE sa_deleted_at IS NULL`,
			`SELECT set_config('claimius.replay_mode', 'true', true)`,
			`INSERT INTO claimius.app_cost (module, period_start, quantity, unit, amount, currency, sa_owner_id, sa_created_by)
			 VALUES ('old', '2026-10-30 09:00+00', 10, 'request', 1.5, 'NOK', gen_random_uuid(), gen_random_uuid())`,
			`SELECT set_config('claimius.replay_mode', 'false', true)`,
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
		if old.Kind != "usage" || !old.PeriodEnd.Equal(old.PeriodStart.Add(time.Hour)) || old.Ref != "2026-10-30T09:00:00Z" || old.Status != "settled" || string(old.Schema) != `{}` || old.UnitPrice != nil {
			t.Fatalf("the realign backfills an alpha0109 row, got %+v", old)
		}
		var indexes []string
		if err := tx.NewRaw(`SELECT indexname FROM pg_indexes WHERE schemaname = 'claimius' AND tablename = 'app_cost' ORDER BY 1`).Scan(ctx, &indexes); err != nil {
			return err
		}
		if strings.Join(indexes, ",") != "app_cost_pkey,idx_app_cost_module_ref,idx_app_cost_period,idx_app_cost_supersedes" {
			t.Fatalf("the realign swaps the unique index, got %v", indexes)
		}
		var constraints []string
		if err := tx.NewRaw(`SELECT conname FROM pg_constraint WHERE conrelid = 'claimius.app_cost'::regclass ORDER BY 1`).Scan(ctx, &constraints); err != nil {
			return err
		}
		if strings.Contains(strings.Join(constraints, ","), "period_start_check") || !strings.Contains(strings.Join(constraints, ","), "app_cost_period_check") {
			t.Fatalf("the realign drops the hour check and adds the window check, got %v", constraints)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("realign: %v", err)
	}
}
