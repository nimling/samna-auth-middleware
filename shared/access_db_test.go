//go:build dbtest

package shared_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	sdb "github.com/nimling/samna-auth-middleware/shared"
)

var (
	userA      = uuid.MustParse("a0000000-0000-4000-8000-00000000000a")
	userB      = uuid.MustParse("b0000000-0000-4000-8000-00000000000b")
	userC      = uuid.MustParse("c0000000-0000-4000-8000-00000000000c")
	orgRoot    = uuid.MustParse("10000000-0000-4000-8000-000000000001")
	orgAlpha   = uuid.MustParse("10000000-0000-4000-8000-000000000011")
	orgGrand   = uuid.MustParse("10000000-0000-4000-8000-000000000111")
	orgDropped = uuid.MustParse("20000000-0000-4000-8000-000000000001")
	orgKept    = uuid.MustParse("30000000-0000-4000-8000-000000000001")
)

const seed = `
DO $$
DECLARE
    v_a       UUID := 'a0000000-0000-4000-8000-00000000000a';
    v_b       UUID := 'b0000000-0000-4000-8000-00000000000b';
    v_c       UUID := 'c0000000-0000-4000-8000-00000000000c';
    v_root    UUID := '10000000-0000-4000-8000-000000000001';
    v_alpha   UUID := '10000000-0000-4000-8000-000000000011';
    v_grand   UUID := '10000000-0000-4000-8000-000000000111';
    v_dropped UUID := '20000000-0000-4000-8000-000000000001';
    v_kept    UUID := '30000000-0000-4000-8000-000000000001';
    v_claim   UUID;
BEGIN
    PERFORM claimius.init();
    PERFORM claimius.ensure_app_user(p_user_id => v_a, p_first_name => 'Ada', p_external_id => 'ext-a');
    PERFORM claimius.ensure_app_user(p_user_id => v_b, p_first_name => 'Bo', p_external_id => 'ext-b');
    PERFORM claimius.ensure_app_user(p_user_id => v_c, p_first_name => 'Cy', p_external_id => 'ext-c');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by) VALUES
        (v_root, 'Root', 'company', v_root, v_root, 0, v_a),
        (v_dropped, 'Dropped', 'company', v_dropped, v_dropped, 0, v_c),
        (v_kept, 'Kept', 'company', v_kept, v_kept, 0, v_c);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_alpha, 'Alpha', 'company', v_root, v_root, 1, v_a);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_grand, 'Grand', 'company', v_alpha, v_root, 2, v_a);

    v_claim := (claimius.create_claim(p_name => 'Root writer', p_description => NULL, p_sa_access => 6,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(p_claim_id => v_claim, p_sa_owner_id => v_root, p_sa_root_id => v_root,
        p_sa_created_by => v_a, p_object_id => v_root, p_object_type => 'claimius.organization', p_sa_access => 6);
    PERFORM claimius.assign_claim_user(p_claim_id => v_claim, p_user_id => v_a, p_sa_owner_id => v_root, p_sa_created_by => v_a);

    v_claim := (claimius.create_claim(p_name => 'Alpha reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_alpha, p_sa_root_id => v_root, p_sa_created_by => v_a) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(p_claim_id => v_claim, p_sa_owner_id => v_alpha, p_sa_root_id => v_root,
        p_sa_created_by => v_a, p_object_id => v_alpha, p_object_type => 'claimius.organization', p_sa_access => 4);
    PERFORM claimius.assign_claim_user(p_claim_id => v_claim, p_user_id => v_a, p_sa_owner_id => v_alpha, p_sa_created_by => v_a);

    v_claim := (claimius.create_claim(p_name => 'Dropped owner', p_description => NULL, p_sa_access => 15,
        p_sa_owner_id => v_dropped, p_sa_root_id => v_dropped, p_sa_created_by => v_c) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(p_claim_id => v_claim, p_sa_owner_id => v_dropped, p_sa_root_id => v_dropped,
        p_sa_created_by => v_c, p_object_id => v_dropped, p_object_type => 'claimius.organization', p_sa_access => 15);
    PERFORM claimius.assign_claim_user(p_claim_id => v_claim, p_user_id => v_c, p_sa_owner_id => v_dropped, p_sa_created_by => v_c);

    v_claim := (claimius.create_claim(p_name => 'Kept owner', p_description => NULL, p_sa_access => 15,
        p_sa_owner_id => v_kept, p_sa_root_id => v_kept, p_sa_created_by => v_c) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.assign_claim_object(p_claim_id => v_claim, p_sa_owner_id => v_kept, p_sa_root_id => v_kept,
        p_sa_created_by => v_c, p_object_id => v_kept, p_object_type => 'claimius.organization', p_sa_access => 15);
    PERFORM claimius.assign_claim_user(p_claim_id => v_claim, p_user_id => v_c, p_sa_owner_id => v_kept, p_sa_created_by => v_c);

    UPDATE claimius.organization SET sa_deleted_at = now() WHERE id = v_dropped;
END $$;
`

func database(t *testing.T) *sdb.DB {
	t.Helper()
	db, err := sdb.Connect(echo.New().Logger)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	var seeded bool
	if err := db.NewRaw(`SELECT EXISTS (SELECT 1 FROM claimius.organization WHERE id = ?)`, orgRoot).Scan(context.Background(), &seeded); err != nil {
		t.Fatalf("seed probe: %v", err)
	}
	if !seeded {
		if _, err := db.ExecContext(context.Background(), seed); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return db
}

func TestMutationAccessGrantsWriteThroughFarAncestor(t *testing.T) {
	db := database(t)
	ac, err := db.MutationAccess(context.Background(), userA, orgGrand, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("expected write through the root claim, got %v", err)
	}
	if ac.ClaimName == nil || *ac.ClaimName != "Root writer" {
		t.Fatalf("expected the Root writer claim, got %+v", ac)
	}
}

func TestMutationAccessManyResolvesUUIDRequests(t *testing.T) {
	db := database(t)
	set, err := db.MutationAccessMany(context.Background(), []sdb.AccessRequest{
		{UserID: userA, ObjectID: orgGrand, ObjectType: sdb.OBJECT_TYPE_ORGANIZATION, RequiredAccess: sdb.ACCESS_WRITE},
		{UserID: userB, ObjectID: orgGrand, ObjectType: sdb.OBJECT_TYPE_ORGANIZATION, RequiredAccess: sdb.ACCESS_READ},
	})
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	if !set.Has(sdb.OBJECT_TYPE_ORGANIZATION, orgGrand, userA, sdb.ACCESS_WRITE) {
		t.Fatal("expected write for user A on grand")
	}
	if set.Has(sdb.OBJECT_TYPE_ORGANIZATION, orgGrand, userB) {
		t.Fatal("expected no grant for user B on grand")
	}
}

func TestUserAccessResolvesExternalID(t *testing.T) {
	db := database(t)
	for _, target := range []string{"ext-b", userB.String()} {
		u, err := db.UserAccess(context.Background(), userB, target, 0)
		if err != nil {
			t.Fatalf("target %q: %v", target, err)
		}
		if u.TargetUserID != userB {
			t.Fatalf("target %q resolved to %s, expected %s", target, u.TargetUserID, userB)
		}
	}
}

func TestRootAccessSkipsSoftDeletedRoot(t *testing.T) {
	db := database(t)
	ac, err := db.RootAccess(context.Background(), userC, sdb.ACCESS_READ)
	if err != nil {
		t.Fatalf("root access: %v", err)
	}
	if ac.RootID != orgKept {
		t.Fatalf("expected the kept root %s, got %s", orgKept, ac.RootID)
	}
	if ac.RootID == orgDropped {
		t.Fatal("root access resolved to a soft deleted root")
	}
}

func TestInsertRootOrganizationKeepsItsOwnID(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	ac, err := db.MutationAccess(ctx, userA, orgRoot, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	org, err := db.InsertRootOrganization(ctx, ac, &sdb.Organization{Name: "Fresh root", Type: sdb.ORGANIZATION_TYPE_COMPANY})
	if err != nil {
		t.Fatalf("insert root organization: %v", err)
	}
	var stored struct {
		ID          uuid.UUID `bun:"id"`
		OwnerID     uuid.UUID `bun:"sa_owner_id"`
		RootID      uuid.UUID `bun:"sa_root_id"`
		PrivateSeed string    `bun:"private_seed"`
	}
	if err := db.NewRaw(`SELECT id, sa_owner_id, sa_root_id, private_seed FROM claimius.organization WHERE id = ?`, org.ID).Scan(ctx, &stored); err != nil {
		t.Fatalf("read back %s: %v", org.ID, err)
	}
	if stored.OwnerID != org.ID || stored.RootID != org.ID {
		t.Fatalf("root %s must own and root itself, got owner %s root %s", org.ID, stored.OwnerID, stored.RootID)
	}
}

func TestDeleteTakesTheRecordIDFromTheModel(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	ac, err := db.MutationAccess(ctx, userA, orgRoot, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	org, err := db.InsertRootOrganization(ctx, ac, &sdb.Organization{Name: "Deleted by model", Type: sdb.ORGANIZATION_TYPE_COMPANY})
	if err != nil {
		t.Fatalf("insert root organization: %v", err)
	}
	owner, err := db.MutationAccess(ctx, userA, org.ID, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access on the new root: %v", err)
	}
	target := sdb.Organization{ID: org.ID}
	if err := db.NewDelete().Model(&target).Access(owner).Scan(ctx, &target); err != nil {
		t.Fatalf("delete by model id: %v", err)
	}
	var deleted bool
	if err := db.NewRaw(`SELECT sa_deleted_at IS NOT NULL FROM claimius.organization WHERE id = ?`, org.ID).Scan(ctx, &deleted); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !deleted {
		t.Fatalf("organization %s was not soft deleted", org.ID)
	}
}
