package shared_test

import (
	"database/sql"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	sdb "github.com/nimling/samna-auth-middleware/shared"
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
	got := sdb.ObjectWhere(db.NewSelect().Model(&orgs), uuid.Nil, "claimius.organization'); SELECT 1; --", sdb.ACCESS_READ, nil, 1).String()
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
