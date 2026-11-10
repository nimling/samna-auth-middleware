//go:build dbtest

package shared_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	sdb "github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
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
    PERFORM claimius.upsert_disciple_state();
    PERFORM claimius.upsert_app_user(p_user_id => v_a, p_first_name => 'Ada', p_external_id => 'ext-a');
    PERFORM claimius.upsert_app_user(p_user_id => v_b, p_first_name => 'Bo', p_external_id => 'ext-b');
    PERFORM claimius.upsert_app_user(p_user_id => v_c, p_first_name => 'Cy', p_external_id => 'ext-c');

    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by) VALUES
        (v_root, 'Root', 'company', v_root, v_root, 0, v_a),
        (v_dropped, 'Dropped', 'company', v_dropped, v_dropped, 0, v_c),
        (v_kept, 'Kept', 'company', v_kept, v_kept, 0, v_c);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_alpha, 'Alpha', 'company', v_root, v_root, 1, v_a);
    INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
    VALUES (v_grand, 'Grand', 'company', v_alpha, v_root, 2, v_a);

    v_claim := (claimius.insert_claim(p_name => 'Root writer', p_description => NULL, p_sa_access => 6,
        p_sa_owner_id => v_root, p_sa_root_id => v_root, p_sa_created_by => v_a) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(p_claim_id => v_claim, p_sa_owner_id => v_root, p_sa_root_id => v_root,
        p_sa_created_by => v_a, p_object_id => v_root, p_object_type => 'claimius.organization', p_sa_access => 6);
    PERFORM claimius.insert_user_claim(p_claim_id => v_claim, p_user_id => v_a, p_sa_owner_id => v_root, p_sa_created_by => v_a);

    v_claim := (claimius.insert_claim(p_name => 'Alpha reader', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_alpha, p_sa_root_id => v_root, p_sa_created_by => v_a) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(p_claim_id => v_claim, p_sa_owner_id => v_alpha, p_sa_root_id => v_root,
        p_sa_created_by => v_a, p_object_id => v_alpha, p_object_type => 'claimius.organization', p_sa_access => 4);
    PERFORM claimius.insert_user_claim(p_claim_id => v_claim, p_user_id => v_a, p_sa_owner_id => v_alpha, p_sa_created_by => v_a);

    v_claim := (claimius.insert_claim(p_name => 'Dropped owner', p_description => NULL, p_sa_access => 15,
        p_sa_owner_id => v_dropped, p_sa_root_id => v_dropped, p_sa_created_by => v_c) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(p_claim_id => v_claim, p_sa_owner_id => v_dropped, p_sa_root_id => v_dropped,
        p_sa_created_by => v_c, p_object_id => v_dropped, p_object_type => 'claimius.organization', p_sa_access => 15);
    PERFORM claimius.insert_user_claim(p_claim_id => v_claim, p_user_id => v_c, p_sa_owner_id => v_dropped, p_sa_created_by => v_c);

    v_claim := (claimius.insert_claim(p_name => 'Kept owner', p_description => NULL, p_sa_access => 15,
        p_sa_owner_id => v_kept, p_sa_root_id => v_kept, p_sa_created_by => v_c) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(p_claim_id => v_claim, p_sa_owner_id => v_kept, p_sa_root_id => v_kept,
        p_sa_created_by => v_c, p_object_id => v_kept, p_object_type => 'claimius.organization', p_sa_access => 15);
    PERFORM claimius.insert_user_claim(p_claim_id => v_claim, p_user_id => v_c, p_sa_owner_id => v_kept, p_sa_created_by => v_c);

    v_claim := (claimius.insert_claim(p_name => 'Time report', p_description => NULL, p_sa_access => 4,
        p_sa_owner_id => v_kept, p_sa_root_id => v_kept, p_sa_created_by => v_c) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(p_claim_id => v_claim, p_sa_owner_id => v_kept, p_sa_root_id => v_kept,
        p_sa_created_by => v_c, p_scope => '{"page": {"time-report": {}}}'::JSONB);
    PERFORM claimius.insert_user_claim(p_claim_id => v_claim, p_user_id => v_b, p_sa_owner_id => v_kept, p_sa_created_by => v_c);
    PERFORM claimius.insert_user_claim(p_claim_id => v_claim, p_user_id => v_c, p_sa_owner_id => v_kept, p_sa_created_by => v_c);

    v_claim := (claimius.insert_claim(p_name => 'No time report', p_description => NULL, p_sa_access => 20,
        p_sa_owner_id => v_kept, p_sa_root_id => v_kept, p_sa_created_by => v_c) -> 'claim' ->> 'id')::UUID;
    PERFORM claimius.insert_claim_object(p_claim_id => v_claim, p_sa_owner_id => v_kept, p_sa_root_id => v_kept,
        p_sa_created_by => v_c, p_scope => '{"page": {"time-report": {}}}'::JSONB);
    PERFORM claimius.insert_user_claim(p_claim_id => v_claim, p_user_id => v_c, p_sa_owner_id => v_kept, p_sa_created_by => v_c);

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
	ac, err := db.RootAccess(context.Background(), userC, uuid.Nil, sdb.ACCESS_READ)
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
	org, err := db.InsertRootOrganization(ctx, nil, ac, &sdb.Organization{Name: "Fresh root", Type: sdb.ORGANIZATION_TYPE_COMPANY})
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
	org := sdb.Organization{Name: "Deleted by model", Type: sdb.ORGANIZATION_TYPE_ENTITY, OwnerID: orgRoot}
	if err := db.NewInsert().Model(&org).Access(ac).Scan(ctx, &org); err != nil {
		t.Fatalf("insert child organization: %v", err)
	}
	owner, err := db.MutationAccess(ctx, userA, org.ID, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access on the new child: %v", err)
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

func TestUserAccessSeparatesMissingFromForbidden(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	if _, err := db.UserAccess(ctx, userA, uuid.Nil.String(), sdb.ACCESS_READ); !errors.Is(err, sdb.ErrNotFound) {
		t.Fatalf("an unknown user must be not found, got %v", err)
	}
	if _, err := db.UserAccess(ctx, userB, "ext-c", sdb.ACCESS_WRITE); !errors.Is(err, sdb.ErrNoAccess) {
		t.Fatalf("an existing user out of reach must be forbidden, got %v", err)
	}
}

func TestDebugSeedEncryptsAndResolvesASecret(t *testing.T) {
	t.Setenv("SAUTH_DEBUG_PRIVATE_SEED", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	t.Setenv("SAUTH_APP_SLUG", "debug-disciple")
	t.Setenv("SAUTH_DEBUG_USER_ID", userA.String())
	db := database(t)
	ctx := context.Background()
	ac, err := db.MutationAccess(ctx, userA, orgRoot, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	secret := sdb.SamnaSecret{Key: "debug_seed_" + uuid.NewString(), Value: "plain", OwnerID: orgRoot}
	if err := db.NewInsert().Model(&secret).Access(ac).Scan(ctx, &secret); err != nil {
		t.Fatalf("insert secret: %v", err)
	}
	if secret.Value == "plain" {
		t.Fatal("secret value was stored unencrypted")
	}
	plain, err := secret.Resolve(ctx, db, userA)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if plain != "plain" {
		t.Fatalf("resolved %q, expected plain", plain)
	}
}

func TestUpdateTakesTheRecordIDFromTheModel(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	ac, err := db.MutationAccess(ctx, userA, orgGrand, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	name := "Grand " + uuid.NewString()[:8]
	target := sdb.Organization{ID: orgGrand, Name: name}
	if err := db.NewUpdate().Model(&target).Access(ac).Scan(ctx, &target); err != nil {
		t.Fatalf("update by model id: %v", err)
	}
	var stored string
	if err := db.NewRaw(`SELECT name FROM claimius.organization WHERE id = ?`, orgGrand).Scan(ctx, &stored); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if stored != name {
		t.Fatalf("expected name %q, got %q", name, stored)
	}
}

func TestInsertRootOrganizationBindsNoForeignClaim(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	ac, err := db.MutationAccess(ctx, userA, orgRoot, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	org, err := db.InsertRootOrganization(ctx, nil, ac, &sdb.Organization{Name: "Unbound root", Type: sdb.ORGANIZATION_TYPE_COMPANY})
	if err != nil {
		t.Fatalf("insert root organization: %v", err)
	}
	var bindings int
	if err := db.NewRaw(`SELECT count(*) FROM claimius.claim_object WHERE object_id = ? AND sa_deleted_at IS NULL`, org.ID).Scan(ctx, &bindings); err != nil {
		t.Fatalf("count bindings: %v", err)
	}
	if bindings != 0 {
		t.Fatalf("a new root must carry no binding to the actor's claim until its owner claim is created, found %d", bindings)
	}
}

func TestGetOrganizationsPagesInKeyOrder(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	all, err := db.GetOrganizations(ctx, userA, sdb.ACCESS_READ, nil, 0)
	if err != nil {
		t.Fatalf("unpaged: %v", err)
	}
	if len(all) < 3 {
		t.Fatalf("expected root, alpha and grand, got %d", len(all))
	}
	var walked []uuid.UUID
	var after *uuid.UUID
	for page := 0; page <= len(all); page++ {
		orgs, err := db.GetOrganizations(ctx, userA, sdb.ACCESS_READ, after, 2)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if len(orgs) > 2 {
			t.Fatalf("page %d: %d rows over a limit of 2", page, len(orgs))
		}
		if len(orgs) == 0 {
			break
		}
		for _, o := range orgs {
			walked = append(walked, o.ID)
		}
		last := orgs[len(orgs)-1].ID
		after = &last
	}
	if len(walked) != len(all) {
		t.Fatalf("pages returned %d organizations, unpaged %d", len(walked), len(all))
	}
	for i := range all {
		if walked[i] != all[i].ID {
			t.Fatalf("position %d: paged %s, unpaged %s", i, walked[i], all[i].ID)
		}
		if i > 0 && walked[i].String() <= walked[i-1].String() {
			t.Fatalf("position %d: %s does not follow %s in key order", i, walked[i], walked[i-1])
		}
	}
}

func TestScopeAccessInheritsTheClaimMaskAndSubtractsADeny(t *testing.T) {
	db := database(t)
	granted, err := db.ScopeAccess(context.Background(), userB, `$.page."time-report"`, sdb.ACCESS_READ)
	if err != nil {
		t.Fatalf("scope access: %v", err)
	}
	if len(granted) != 1 || granted[0].Access != sdb.ACCESS_READ {
		t.Fatalf("expected one read grant through a binding with no mask of its own, got %+v", granted)
	}
	denied, err := db.ScopeAccess(context.Background(), userC, `$.page."time-report"`, sdb.ACCESS_READ)
	if err != nil {
		t.Fatalf("scope access: %v", err)
	}
	if len(denied) != 0 {
		t.Fatalf("expected the deny on the same path to take the read away, got %+v", denied)
	}
}

func TestObjectWhereWithAnIDChecksInTheSameStatement(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	read := func(user uuid.UUID) int {
		orgs := make([]sdb.Organization, 0)
		if err := sdb.ObjectWhere(db.NewSelect().Model(&orgs).ExcludeColumn("sa_deleted_at"), user, string(sdb.OBJECT_TYPE_ORGANIZATION), sdb.ACCESS_READ, &orgGrand, nil, 0).Scan(ctx); err != nil {
			t.Fatalf("read: %v", err)
		}
		return len(orgs)
	}
	if n := read(userA); n != 1 {
		t.Fatalf("a reader with a grant reads the one organization, got %d rows", n)
	}
	if n := read(userB); n != 0 {
		t.Fatalf("a reader without a grant reads nothing, got %d rows", n)
	}
}

func TestUsageFlushTouchesTheCheckedPair(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	if _, err := db.GetOrganizations(ctx, userA, sdb.ACCESS_READ, nil, 0); err != nil {
		t.Fatalf("list: %v", err)
	}
	var before int64
	if err := db.NewRaw(`SELECT used_count FROM claimius.user_object WHERE user_id = ? AND object_type = ? AND object_id = ?`,
		userA, sdb.OBJECT_TYPE_ORGANIZATION, orgGrand).Scan(ctx, &before); err != nil {
		t.Fatalf("pair: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := db.MutationAccess(ctx, userA, orgGrand, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_READ); err != nil {
			t.Fatalf("check: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	db = database(t)
	if _, err := db.GetOrganizations(ctx, userA, sdb.ACCESS_READ, nil, 0); err != nil {
		t.Fatalf("list: %v", err)
	}
	var after int64
	if err := db.NewRaw(`SELECT used_count FROM claimius.user_object WHERE user_id = ? AND object_type = ? AND object_id = ?`,
		userA, sdb.OBJECT_TYPE_ORGANIZATION, orgGrand).Scan(ctx, &after); err != nil {
		t.Fatalf("pair: %v", err)
	}
	if after-before != 2 {
		t.Fatalf("expected two counted checks on the pair, got %d", after-before)
	}
}

func TestOwnerGraphTakesTheWindow(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	if _, err := db.GetOwnerGraph(ctx, userA, &orgRoot, 0, sdb.ACCESS_READ, sdb.GraphWindow{Width: 1, Sort: sdb.GRAPH_SORT_USAGE}); err != nil {
		t.Fatalf("usage window: %v", err)
	}
	nodes, err := db.GetOwnerGraph(ctx, userA, &orgRoot, 0, sdb.ACCESS_READ, sdb.GraphWindow{Width: 1, NextID: &orgAlpha})
	if err != nil {
		t.Fatalf("continuation: %v", err)
	}
	for _, n := range nodes {
		if n.ObjectID == orgAlpha || n.ObjectID == orgRoot {
			t.Fatalf("a continuation after alpha returns neither alpha nor its parent, got %+v", n)
		}
	}
	if _, err := db.GetOwnerGraph(ctx, userA, &orgRoot, 0, sdb.ACCESS_READ, sdb.GraphWindow{Sort: "size"}); err == nil {
		t.Fatal("an unknown sort must be refused")
	}
}

func TestClaimGraphReadWaitsOnNoWriterHoldingItsCacheRow(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	if _, err := db.GetClaimGraph(ctx, userA, nil, 0, 0, sdb.GraphWindow{}); err != nil {
		t.Fatalf("build cache: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE claimius.claim_graph_cache SET snapshot = '1:1:'::pg_snapshot WHERE user_id = ?`, userA); err != nil {
		t.Fatalf("stale cache: %v", err)
	}
	var grant uuid.UUID
	if err := db.NewRaw(`SELECT id FROM claimius.user_claim WHERE user_id = ? AND sa_deleted_at IS NULL ORDER BY id LIMIT 1`, userA).Scan(ctx, &grant); err != nil {
		t.Fatalf("grant: %v", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT claimius.insert_claim(p_name => 'Held writer', p_description => NULL, p_sa_access => 4,
		p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?)`, orgRoot, orgRoot, grant); err != nil {
		t.Fatalf("writer: %v", err)
	}
	var cached int
	if err := tx.NewRaw(`SELECT count(*) FROM claimius.claim_graph_cache WHERE user_id = ?`, userA).Scan(ctx, &cached); err != nil {
		t.Fatalf("cache probe: %v", err)
	}
	if cached != 0 {
		t.Fatalf("expected the writer to drop the reader's cache row, %d left", cached)
	}
	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := db.GetClaimGraph(readCtx, userA, nil, 0, 0, sdb.GraphWindow{}); err != nil {
		t.Fatalf("a graph read must not wait on a writer holding its cache row: %v", err)
	}
}

func TestAccessDiffDrainRunsOffTheRequestPath(t *testing.T) {
	db := database(t)
	if _, err := db.PostAccessDiffDrain(context.Background(), 10); err != nil {
		t.Fatalf("drain: %v", err)
	}
}

func TestReadRunsOnAReadOnlyReplicaAndRetriesARefusedWrite(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	db.Replica = bun.NewDB(sql.OpenDB(pgdriver.NewConnector(
		pgdriver.WithDSN(db.ConnStr),
		pgdriver.WithConnParams(map[string]any{"default_transaction_read_only": "on"}),
	)), pgdialect.New())
	defer func() {
		db.Replica.Close()
		db.Replica = nil
	}()
	granted, err := db.ScopeAccess(ctx, userB, `$.page."time-report"`, sdb.ACCESS_READ)
	if err != nil || len(granted) != 1 {
		t.Fatalf("expected the scope read to answer on the read only replica, got %+v, %v", granted, err)
	}
	var ran []bool
	err = db.Read(ctx, func(ctx context.Context, b *bun.DB) error {
		ran = append(ran, b == db.Replica)
		_, err := b.NewRaw(`CREATE TEMP TABLE IF NOT EXISTS read_route_probe (id INTEGER)`).Exec(ctx)
		return err
	})
	if err != nil {
		t.Fatalf("expected the refused write to succeed on the primary: %v", err)
	}
	if len(ran) != 2 || !ran[0] || ran[1] {
		t.Fatalf("expected the replica to refuse and the primary to answer, got %v", ran)
	}
}

func TestASystemUserReadsItselfAndStaysHiddenFromOthers(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	system := uuid.MustParse("d0000000-0000-4000-8000-00000000000d")
	if _, err := db.NewRaw(`SELECT claimius.upsert_app_user(p_user_id => ?, p_first_name => 'Root')`, system).Exec(ctx); err != nil {
		t.Fatalf("seed system user: %v", err)
	}
	if _, err := db.NewRaw(`UPDATE claimius.samna_user SET type = 'system' WHERE user_id = ?`, system).Exec(ctx); err != nil {
		t.Fatalf("mark system user: %v", err)
	}
	self, err := db.GetUser(ctx, system, system)
	if err != nil || self.UserID != system {
		t.Fatalf("a system user must read its own row, got %+v %v", self, err)
	}
	if _, err := db.GetUser(ctx, userA, system); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("a system user must stay hidden from other users, got %v", err)
	}
}

func TestGetAccessReadsThePairAfterItsMark(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	if _, err := db.GetAccess(ctx, userA, orgGrand, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE); err != nil {
		t.Fatalf("first check: %v", err)
	}
	const entries = `SELECT sum(pg_stat_get_xact_tuples_returned(i.indexrelid))
		FROM pg_index i JOIN pg_class c ON c.oid = i.indrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'claimius'`
	var read int64
	err := db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var before, after int64
		if err := tx.NewRaw(entries).Scan(ctx, &before); err != nil {
			return err
		}
		claims := make([]sdb.AccessClaim, 0)
		if err := tx.NewRaw("SELECT * FROM claimius.get_access(?, ?, ?, ?)", userA, orgGrand, sdb.OBJECT_TYPE_ORGANIZATION, int(sdb.ACCESS_WRITE)).Scan(ctx, &claims); err != nil {
			return err
		}
		if len(claims) != 1 || claims[0].ClaimName == nil || *claims[0].ClaimName != "Root writer" {
			t.Fatalf("expected the Root writer grant, got %+v", claims)
		}
		if err := tx.NewRaw(entries).Scan(ctx, &after); err != nil {
			return err
		}
		read = after - before
		return nil
	})
	if err != nil {
		t.Fatalf("counted check: %v", err)
	}
	if read > 8 {
		t.Fatalf("a check with nothing raised since the pair's mark read %d index entries, expected at most 8", read)
	}
}

func TestClaimUnderSoftDeletedOrganizationAgreesWithItsGate(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	ac, err := db.MutationAccess(ctx, userA, orgRoot, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	org := sdb.Organization{Name: "Composer owner", Type: sdb.ORGANIZATION_TYPE_ENTITY, OwnerID: orgRoot}
	if err := db.NewInsert().Model(&org).Access(ac).Scan(ctx, &org); err != nil {
		t.Fatalf("insert child organization: %v", err)
	}
	owner, err := db.MutationAccess(ctx, userA, org.ID, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access on the new child: %v", err)
	}
	var claimID uuid.UUID
	if err := db.NewRaw(`SELECT (claimius.insert_claim(p_name => 'Composer claim', p_description => NULL, p_sa_access => 15,
		p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?) -> 'claim' ->> 'id')::uuid`,
		org.ID, orgRoot, owner.UserClaimID).Scan(ctx, &claimID); err != nil {
		t.Fatalf("insert claim: %v", err)
	}
	agree := func(stage string, want bool) {
		t.Helper()
		claims, err := db.GetClaims(ctx, userA, sdb.ACCESS_READ)
		if err != nil {
			t.Fatalf("%s: list claims: %v", stage, err)
		}
		listed := false
		for _, c := range claims {
			if c.ID == claimID {
				listed = true
			}
		}
		_, gateErr := db.MutationAccess(ctx, userA, claimID, sdb.OBJECT_TYPE_CLAIM, sdb.ACCESS_READ)
		if gateErr != nil && !errors.Is(gateErr, sdb.ErrNoAccess) {
			t.Fatalf("%s: gate: %v", stage, gateErr)
		}
		if listed != want || (gateErr == nil) != want {
			t.Fatalf("%s: listed %v, gate passed %v, want both %v", stage, listed, gateErr == nil, want)
		}
	}
	agree("before the delete", true)
	target := sdb.Organization{ID: org.ID}
	if err := db.NewDelete().Model(&target).Access(owner).Scan(ctx, &target); err != nil {
		t.Fatalf("soft delete the owner organization: %v", err)
	}
	agree("after the owner's soft delete", false)
}

func TestCreatorReachesADraftWithNoOwnerByID(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS public.gotest_draft (
			id UUID PRIMARY KEY, name TEXT, sa_owner_id UUID, sa_created_by UUID,
			sa_deleted_at TIMESTAMPTZ, sa_created_at TIMESTAMPTZ NOT NULL DEFAULT now(), sa_updated_at TIMESTAMPTZ NOT NULL DEFAULT now())
	`); err != nil {
		t.Fatalf("draft table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		SELECT claimius.upsert_table_info(VARIADIC (SELECT coalesce(array_agg(object_type), '{}') FROM claimius.table_info WHERE NOT is_internal AND object_type <> 'public.gotest_draft') || 'public.gotest_draft'::TEXT)
	`); err != nil {
		t.Fatalf("register draft table: %v", err)
	}
	var grant uuid.UUID
	if err := db.NewRaw(`SELECT id FROM claimius.user_claim WHERE user_id = ? AND sa_deleted_at IS NULL ORDER BY sa_created_at LIMIT 1`, userA).Scan(ctx, &grant); err != nil {
		t.Fatalf("creator grant: %v", err)
	}
	draft := uuid.New()
	if _, err := db.ExecContext(ctx, `INSERT INTO public.gotest_draft (id, name, sa_owner_id, sa_created_by) VALUES (?, 'Draft', NULL, ?)`, draft, grant); err != nil {
		t.Fatalf("insert draft: %v", err)
	}

	var listed []struct {
		ObjectID    uuid.UUID `bun:"object_id"`
		SaAccess    int       `bun:"sa_access"`
		DirectGrant bool      `bun:"direct_grant"`
	}
	if err := db.NewRaw(`SELECT object_id, sa_access, direct_grant FROM claimius.get_objects(?, 'public.gotest_draft', 15) WHERE object_id = ?`, userA, draft).Scan(ctx, &listed); err != nil {
		t.Fatalf("get_objects: %v", err)
	}
	if len(listed) != 1 || !listed[0].DirectGrant || listed[0].SaAccess&15 != 15 {
		t.Fatalf("the creator lists the draft through direct_grant at 15, got %+v", listed)
	}

	for _, required := range []sdb.ClaimAccess{sdb.ACCESS_READ, sdb.ACCESS_WRITE, sdb.ACCESS_OWNER} {
		ac, err := db.MutationAccess(ctx, userA, draft, "public.gotest_draft", required)
		if err != nil {
			t.Fatalf("mutation_access at %d: the creator keeps the draft by id, got %v", required, err)
		}
		if !ac.DirectGrant || ac.UserClaimID != nil || int(ac.Access)&int(required) != int(required) {
			t.Fatalf("mutation_access at %d answers the direct grant, got %+v", required, ac)
		}
		got, err := db.GetAccess(ctx, userA, draft, "public.gotest_draft", required)
		if err != nil || got == nil || !got.DirectGrant {
			t.Fatalf("get_access at %d answers the direct grant, got %+v %v", required, got, err)
		}
	}
	if _, err := db.MutationAccess(ctx, userB, draft, "public.gotest_draft", sdb.ACCESS_READ); !errors.Is(err, sdb.ErrNoAccess) {
		t.Fatalf("another user is refused the draft, got %v", err)
	}
}

func TestLocalDeleteRemovesAScopeBindingAndAGrantByTheirOwnID(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	ac, err := db.MutationAccess(ctx, userA, orgRoot, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	claim, err := db.CreateClaim(ctx, nil, ac, sdb.CreateClaimParams{Name: "Local scoped pages", Access: sdb.ACCESS_READ})
	if err != nil {
		t.Fatalf("create claim: %v", err)
	}
	scope := json.RawMessage(`{"page": {"hr": {"_access": "READ"}}}`)
	binding, err := claim.AssignObject(&sdb.ClaimObject{Scope: &scope, Inherits: true, OwnerID: orgRoot, RootID: orgRoot})
	if err != nil {
		t.Fatalf("scope binding: %v", err)
	}
	if binding.ObjectID != nil {
		t.Fatalf("a scope binding carries no object, got %v", *binding.ObjectID)
	}
	grants, err := claim.AssignUser(userB)
	if err != nil || len(grants) != 1 {
		t.Fatalf("grant: %+v %v", grants, err)
	}

	live := func(table string, id uuid.UUID) bool {
		t.Helper()
		var found bool
		if err := db.NewRaw(`SELECT EXISTS (SELECT 1 FROM claimius.`+table+` WHERE id = ? AND sa_deleted_at IS NULL)`, id).Scan(ctx, &found); err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		return found
	}

	if _, err := db.NewDelete().Model(&sdb.ClaimObject{ID: binding.ID, ClaimID: claim.ID}).Access(ac).Exec(ctx); err != nil {
		t.Fatalf("delete scope binding: %v", err)
	}
	if live("claim_object", binding.ID) {
		t.Fatal("a scope binding deleted by its id is gone")
	}
	if _, err := db.NewDelete().Model(&sdb.UserClaim{ID: grants[0].ID, ClaimID: claim.ID}).Access(ac).Exec(ctx); err != nil {
		t.Fatalf("delete grant: %v", err)
	}
	if live("user_claim", grants[0].ID) {
		t.Fatal("a grant deleted by its id is gone")
	}
	if _, err := db.NewDelete().Model(&sdb.ClaimObject{ID: uuid.New(), ClaimID: claim.ID}).Access(ac).Exec(ctx); !errors.Is(err, sdb.ErrNotFound) {
		t.Fatalf("a binding id the claim does not hold answers ErrNotFound, got %v", err)
	}
}

func TestUpdateOrganizationMovesItUnderAnotherParent(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	ac, err := db.MutationAccess(ctx, userA, orgRoot, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	insert := func(name string, owner uuid.UUID) uuid.UUID {
		org := sdb.Organization{Name: name, Type: sdb.ORGANIZATION_TYPE_ENTITY, OwnerID: owner}
		if err := db.NewInsert().Model(&org).Access(ac).Scan(ctx, &org); err != nil {
			t.Fatalf("insert %s: %v", name, err)
		}
		return org.ID
	}
	mover := insert("Mover", orgRoot)
	sibling := insert("Sibling", orgRoot)
	child := insert("Mover child", mover)
	organization := sdb.ObjectType(sdb.OBJECT_TYPE_ORGANIZATION)
	grant := func(name string, access sdb.ClaimAccess, object uuid.UUID, user uuid.UUID) {
		claim, err := db.CreateClaim(ctx, nil, ac, sdb.CreateClaimParams{Name: name, Access: access})
		if err != nil {
			t.Fatalf("claim %s: %v", name, err)
		}
		if _, err := claim.AssignObject(&sdb.ClaimObject{ObjectID: &object, ObjectType: &organization, Inherits: true, OwnerID: orgRoot, RootID: orgRoot}); err != nil {
			t.Fatalf("bind %s: %v", name, err)
		}
		if _, err := claim.AssignUser(user); err != nil {
			t.Fatalf("assign %s: %v", name, err)
		}
	}
	grant("Sibling reader", sdb.ACCESS_READ, sibling, userC)
	grant("Mover writer", sdb.ACCESS_READ|sdb.ACCESS_WRITE, mover, userB)
	move := func(actor uuid.UUID, id, owner uuid.UUID) error {
		access, err := db.MutationAccess(ctx, actor, id, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
		if err != nil {
			return err
		}
		target := sdb.Organization{ID: id, OwnerID: owner}
		return db.NewUpdate().Model(&target).Access(access).Scan(ctx, &target)
	}
	ownerOf := func(id uuid.UUID) uuid.UUID {
		var owner uuid.UUID
		if err := db.NewRaw(`SELECT sa_owner_id FROM claimius.organization WHERE id = ?`, id).Scan(ctx, &owner); err != nil {
			t.Fatalf("read owner: %v", err)
		}
		return owner
	}

	if db.HasAccess(ctx, userC, child, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_READ) {
		t.Fatal("the sibling's reader must not read the mover's child before the move")
	}
	if err := move(userB, mover, sibling); !errors.Is(err, sdb.ErrNoAccess) {
		t.Fatalf("a caller without write on the new parent must be refused, got %v", err)
	}
	if owner := ownerOf(mover); owner != orgRoot {
		t.Fatalf("a refused move must leave the owner, got %s", owner)
	}
	if err := move(userA, mover, sibling); err != nil {
		t.Fatalf("move under the sibling: %v", err)
	}
	if owner := ownerOf(mover); owner != sibling {
		t.Fatalf("the mover must be owned by the sibling, got %s", owner)
	}
	ancestors, err := db.GetParentOrganizations(ctx, child)
	if err != nil {
		t.Fatalf("ancestors: %v", err)
	}
	if !slices.Contains(ancestors, sibling) || !slices.Contains(ancestors, mover) {
		t.Fatalf("the mover's child must follow it under the sibling, got %v", ancestors)
	}
	if !db.HasAccess(ctx, userC, child, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_READ) {
		t.Fatal("the sibling's reader must read the mover's child after the move")
	}
	if err := move(userA, sibling, child); !errors.Is(err, sdb.ErrOrganizationParent) {
		t.Fatalf("a move under a descendant must be refused, got %v", err)
	}
	if err := move(userA, sibling, sibling); !errors.Is(err, sdb.ErrOrganizationParent) {
		t.Fatalf("a move under itself must be refused, got %v", err)
	}
	if owner := ownerOf(sibling); owner != orgRoot {
		t.Fatalf("a refused cycle must leave the owner, got %s", owner)
	}
}
