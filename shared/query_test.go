package shared_test

import (
	"database/sql"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	sdb "github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

func TestStampAccessClaim_StampsPointerAndValueCreators(t *testing.T) {
	userClaimID := uuid.New()
	ac := &sdb.AccessClaim{UserClaimID: &userClaimID, OwnerID: uuid.New()}

	claim := &sdb.Claim{}
	sdb.StampAccessClaim(claim, ac)
	if claim.CreatedBy == nil || *claim.CreatedBy != userClaimID {
		t.Fatalf("claim created_by: want %s, got %v", userClaimID, claim.CreatedBy)
	}

	binding := &sdb.ClaimObject{}
	sdb.StampAccessClaim(binding, ac)
	if binding.CreatedBy == nil || *binding.CreatedBy != userClaimID {
		t.Fatalf("claim_object created_by: want %s, got %v", userClaimID, binding.CreatedBy)
	}

	secret := &sdb.SamnaSecret{}
	sdb.StampAccessClaim(secret, ac)
	if secret.CreatedBy == nil || *secret.CreatedBy != userClaimID {
		t.Fatalf("samna_secret created_by: want %s, got %v", userClaimID, secret.CreatedBy)
	}

	org := &sdb.Organization{}
	sdb.StampAccessClaim(org, ac)
	if org.CreatedBy != userClaimID {
		t.Fatalf("organization created_by: want %s, got %s", userClaimID, org.CreatedBy)
	}
}

func TestStampAccessClaim_KeepsAnExplicitCreator(t *testing.T) {
	userClaimID := uuid.New()
	explicit := uuid.New()
	ac := &sdb.AccessClaim{UserClaimID: &userClaimID}

	claim := &sdb.Claim{CreatedBy: &explicit}
	sdb.StampAccessClaim(claim, ac)
	if *claim.CreatedBy != explicit {
		t.Fatalf("claim created_by: want the explicit %s, got %s", explicit, *claim.CreatedBy)
	}
}

func offlineDB() *sdb.DB {
	conn := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN("postgres://offline@127.0.0.1:1/offline?sslmode=disable")))
	return &sdb.DB{DB: bun.NewDB(conn, pgdialect.New())}
}

func TestGetOrganizationsQuery_BindsThePage(t *testing.T) {
	db := offlineDB()
	userID := uuid.MustParse("a0000000-0000-4000-8000-00000000000a")
	after := uuid.MustParse("10000000-0000-4000-8000-000000000011")
	orgs := make([]sdb.Organization, 0)
	got := db.GetOrganizationsQuery(&orgs, userID, sdb.ACCESS_READ, &after, 50).String()
	want := `"organization".id IN (SELECT object_id FROM claimius.get_objects('a0000000-0000-4000-8000-00000000000a', 'claimius.organization', 4, '10000000-0000-4000-8000-000000000011', 50))`
	if !strings.Contains(got, want) {
		t.Fatalf("page not bound into get_objects:\nwant %s\ngot  %s", want, got)
	}
	if !strings.HasSuffix(got, `ORDER BY "organization".id`) {
		t.Fatalf("page not ordered by id: %s", got)
	}
}

func TestGetLocationsQuery_UnpagedPassesNull(t *testing.T) {
	db := offlineDB()
	userID := uuid.MustParse("a0000000-0000-4000-8000-00000000000a")
	locs := make([]sdb.Location, 0)
	got := db.GetLocationsQuery(&locs, userID, sdb.ACCESS_NONE, nil, 0).String()
	want := `"location".id IN (SELECT object_id FROM claimius.get_objects('a0000000-0000-4000-8000-00000000000a', 'claimius.location', 0, NULL, NULL))`
	if !strings.Contains(got, want) {
		t.Fatalf("unpaged listing must pass NULL after and limit:\nwant %s\ngot  %s", want, got)
	}
}

func TestObjectWhere_QuotesItsArguments(t *testing.T) {
	db := offlineDB()
	orgs := make([]sdb.Organization, 0)
	got := sdb.ObjectWhere(db.NewSelect().Model(&orgs), uuid.Nil, "claimius.organization'); SELECT 1; --", sdb.ACCESS_READ, nil, nil, 1).String()
	if !strings.Contains(got, `'claimius.organization''); SELECT 1; --'`) {
		t.Fatalf("object type not bound as a literal: %s", got)
	}
}

func TestQueryPage(t *testing.T) {
	e := echo.New()
	parse := func(query string) (*uuid.UUID, int, error) {
		req := httptest.NewRequest("GET", "/me/organization?"+query, nil)
		return sdb.QueryPage(e.NewContext(req, httptest.NewRecorder()))
	}

	after, limit, err := parse("")
	if err != nil || after != nil || limit != 0 {
		t.Fatalf("no page: got %v %d %v", after, limit, err)
	}

	after, limit, err = parse("cursor=10000000-0000-4000-8000-000000000011&limit=25")
	if err != nil || after == nil || after.String() != "10000000-0000-4000-8000-000000000011" || limit != 25 {
		t.Fatalf("page: got %v %d %v", after, limit, err)
	}

	for _, bad := range []string{"cursor=nope", "limit=0", "limit=-3", "limit=ten"} {
		if _, _, err := parse(bad); err == nil {
			t.Fatalf("%s: expected an error", bad)
		}
	}
}

func TestObjectWhere_CarriesTheProbeForOneObject(t *testing.T) {
	db := offlineDB()
	userID := uuid.MustParse("a0000000-0000-4000-8000-00000000000a")
	orgID := uuid.MustParse("10000000-0000-4000-8000-000000000011")
	orgs := make([]sdb.Organization, 0)
	got := sdb.ObjectWhere(db.NewSelect().Model(&orgs), userID, "claimius.organization", sdb.ACCESS_WRITE, &orgID, nil, 0).String()
	for _, want := range []string{
		`"organization".id = '10000000-0000-4000-8000-000000000011'`,
		`EXISTS (SELECT 1 FROM claimius.get_access('a0000000-0000-4000-8000-00000000000a', '10000000-0000-4000-8000-000000000011', 'claimius.organization', 2))`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("one object read must carry its check in the statement:\nwant %s\ngot  %s", want, got)
		}
	}
	if strings.Contains(got, "get_objects") {
		t.Fatalf("one object read must not list the reader's range: %s", got)
	}
}

func TestGraphQueries_BindTheWindow(t *testing.T) {
	db := offlineDB()
	userID := uuid.MustParse("a0000000-0000-4000-8000-00000000000a")
	next := uuid.MustParse("10000000-0000-4000-8000-000000000012")
	owner := db.GetOwnerGraphQuery(userID, nil, 2, sdb.ACCESS_READ, sdb.GraphWindow{Width: 25, Sort: sdb.GRAPH_SORT_USAGE, NextID: &next}).String()
	want := `claimius.get_owner_graph(p_user_id := 'a0000000-0000-4000-8000-00000000000a', p_start_id := NULL, p_depth := 2, p_required_access := 4, p_width := 25, p_sort := 'usage', p_next_id := '10000000-0000-4000-8000-000000000012')`
	if !strings.Contains(owner, want) {
		t.Fatalf("owner graph window:\nwant %s\ngot  %s", want, owner)
	}
	claim := db.GetClaimGraphQuery(userID, nil, 0, sdb.ACCESS_NONE, sdb.GraphWindow{}).String()
	want = `claimius.get_claim_graph(p_user_id := 'a0000000-0000-4000-8000-00000000000a', p_start_id := NULL, p_depth := 0, p_required_access := 0, p_width := 0, p_sort := 'name', p_next_id := NULL)`
	if !strings.Contains(claim, want) {
		t.Fatalf("an empty window is every child in name order:\nwant %s\ngot  %s", want, claim)
	}
}

func TestObjectWhere_OrdersOnlyAPage(t *testing.T) {
	db := offlineDB()
	userID := uuid.MustParse("a0000000-0000-4000-8000-00000000000a")
	orgs := make([]sdb.Organization, 0)
	got := sdb.ObjectWhere(db.NewSelect().Model(&orgs).OrderExpr(`"organization".name`), userID, "claimius.organization", sdb.ACCESS_READ, nil, nil, 0).String()
	if !strings.HasSuffix(got, `ORDER BY "organization".name`) {
		t.Fatalf("an unpaged listing must keep the caller's order alone: %s", got)
	}
	after := uuid.MustParse("10000000-0000-4000-8000-000000000011")
	got = sdb.ObjectWhere(db.NewSelect().Model(&orgs), userID, "claimius.organization", sdb.ACCESS_READ, nil, &after, 25).String()
	if !strings.HasSuffix(got, `ORDER BY "organization".id`) {
		t.Fatalf("a page must order on the id its cursor continues from: %s", got)
	}
}

func TestObjectWhere_ResolvesTheTableWithoutAModel(t *testing.T) {
	db := offlineDB()
	userID := uuid.MustParse("a0000000-0000-4000-8000-00000000000a")
	for query, want := range map[*bun.SelectQuery]string{
		db.NewSelect().Table("public.asset").Column("id"):                `"public"."asset".id IN (SELECT object_id FROM claimius.get_objects(`,
		db.NewSelect().TableExpr("public.asset AS a").ColumnExpr("a.id"): `a.id IN (SELECT object_id FROM claimius.get_objects(`,
	} {
		got := sdb.ObjectWhere(query, userID, "public.asset", sdb.ACCESS_READ, nil, nil, 10).String()
		if !strings.Contains(got, want) || strings.Contains(got, "?TableAlias") {
			t.Fatalf("a query without a model must name its table:\nwant %s\ngot  %s", want, got)
		}
	}
}
