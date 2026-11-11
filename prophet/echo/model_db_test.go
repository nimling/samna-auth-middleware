//go:build dbtest

package echo_test

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/google/uuid"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/uptrace/bun/driver/pgdriver"
)

func getEventApps(t *testing.T, eventType string, id uuid.UUID) []uuid.UUID {
	t.Helper()
	apps := make([]uuid.UUID, 0)
	if err := psdb.Instance().NewRaw(`
		SELECT DISTINCT app_id FROM claimius.sync_event
		WHERE event_type = ? AND operation = 'upsert' AND (payload ->> 'id')::UUID = ?
		ORDER BY app_id
	`, eventType, id).Scan(context.Background(), &apps); err != nil {
		t.Fatalf("events of %s %s: %v", eventType, id, err)
	}
	return apps
}

func getSortedApps(apps ...uuid.UUID) []uuid.UUID {
	sort.Slice(apps, func(i, j int) bool { return apps[i].String() < apps[j].String() })
	return apps
}

func getSameApps(got, want []uuid.UUID) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestAUserIsOneRowDeliveredToEveryAppWhereItHoldsALinkedGrant(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	prophet := psdb.Instance()
	first := postPeerApp(t, grantUser)
	second := postPeerApp(t, grantUser)
	outside := postPeerApp(t, grantUser)
	firstClaim := postPeerClaim(t, prophet, first.Org, grantUser, "First reader")
	secondClaim := postPeerClaim(t, prophet, second.Org, grantUser, "Second reader")

	user := uuid.New()
	if _, err := prophet.ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Shared')`, user, first.ID); err != nil {
		t.Fatalf("user through the first app: %v", err)
	}
	for _, claim := range []struct{ id, org uuid.UUID }{{firstClaim, first.Org}, {secondClaim, second.Org}} {
		if _, err := prophet.ExecContext(ctx, `
			SELECT claimius.insert_user_claim(p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?)
		`, claim.id, user, claim.org, grantUser); err != nil {
			t.Fatalf("grant: %v", err)
		}
	}

	var rows int
	if err := prophet.NewRaw(`SELECT count(*) FROM claimius.samna_user WHERE user_id = ? AND sa_deleted_at IS NULL`, user).Scan(ctx, &rows); err != nil || rows != 1 {
		t.Fatalf("a user is one samna_user row whatever app it came through, got %d %v", rows, err)
	}
	var row uuid.UUID
	if err := prophet.NewRaw(`SELECT id FROM claimius.samna_user WHERE user_id = ? AND sa_deleted_at IS NULL`, user).Scan(ctx, &row); err != nil {
		t.Fatalf("row: %v", err)
	}
	want := getSortedApps(first.ID, second.ID)
	routed := make([]uuid.UUID, 0)
	if err := prophet.NewRaw(`SELECT a FROM claimius.apps_for_object('claimius.samna_user', ?) a ORDER BY a`, user).Scan(ctx, &routed); err != nil {
		t.Fatalf("routing: %v", err)
	}
	if !getSameApps(routed, want) {
		t.Fatalf("the user reaches the apps linked to the owners of its grants' claims %v, got %v", want, routed)
	}
	if got := getEventApps(t, "claimius.samna_user", row); !getSameApps(got, want) {
		t.Fatalf("the one row is published to %v and to no other app, got %v", want, got)
	}
	for _, app := range getEventApps(t, "claimius.samna_user", row) {
		if app == outside.ID {
			t.Fatalf("an app no grant of the user is linked to never receives the user")
		}
	}
}

func TestAClaimAndItsBindingsCarryNoAppAndReachEveryAppLinkedToItsOrganization(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	prophet := psdb.Instance()
	home := postPeerApp(t, grantUser)
	sharing := postPeerApp(t, grantUser)
	late := postPeerApp(t, grantUser)
	if _, err := prophet.ExecContext(ctx, `SELECT claimius.upsert_app_object(?, 'claimius.organization', ?, ?)`, sharing.ID, home.Org, grantUser); err != nil {
		t.Fatalf("link the organization to a second app: %v", err)
	}

	claim := postPeerClaim(t, prophet, home.Org, grantUser, "Shared claim")
	var binding uuid.UUID
	if err := prophet.NewRaw(`
		SELECT (claimius.insert_claim_object(p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
		       p_sa_created_by => ?, p_object_id => ?, p_object_type => 'claimius.organization', p_inherits => TRUE) -> 'claim_object' ->> 'id')::UUID
	`, claim, home.Org, home.Org, grantUser, home.Org).Scan(ctx, &binding); err != nil {
		t.Fatalf("bind: %v", err)
	}

	var carries bool
	if err := prophet.NewRaw(`
		SELECT (SELECT jsonb_exists(to_jsonb(c), 'app_id') FROM claimius.claim c WHERE c.id = ?)
		    OR (SELECT jsonb_exists(to_jsonb(co), 'app_id') FROM claimius.claim_object co WHERE co.id = ?)
	`, claim, binding).Scan(ctx, &carries); err != nil || carries {
		t.Fatalf("a claim and its binding carry no app, got %v %v", carries, err)
	}
	want := getSortedApps(home.ID, sharing.ID)
	if got := getEventApps(t, "claimius.claim", claim); !getSameApps(got, want) {
		t.Fatalf("the claim reaches every app linked to its owning organization %v, got %v", want, got)
	}
	if got := getEventApps(t, "claimius.claim_object", binding); !getSameApps(got, want) {
		t.Fatalf("the binding reaches the apps of its claim %v, got %v", want, got)
	}

	if _, err := prophet.ExecContext(ctx, `SELECT claimius.upsert_app_object(?, 'claimius.organization', ?, ?)`, late.ID, home.Org, grantUser); err != nil {
		t.Fatalf("link the organization to a third app: %v", err)
	}
	want = getSortedApps(home.ID, sharing.ID, late.ID)
	if got := getEventApps(t, "claimius.claim", claim); !getSameApps(got, want) {
		t.Fatalf("an app linked afterwards receives the organization's claim, want %v got %v", want, got)
	}
	if got := getEventApps(t, "claimius.claim_object", binding); !getSameApps(got, want) {
		t.Fatalf("an app linked afterwards receives the claim's binding, want %v got %v", want, got)
	}
}

func TestAnOwnerClaimOnARootGivesOwnerInEveryAppTheRootIsLinkedTo(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	prophet := psdb.Instance()
	home := postPeerApp(t, grantUser)
	linked := postPeerApp(t, grantUser)
	unlinked := postPeerApp(t, grantUser)
	if _, err := prophet.ExecContext(ctx, `SELECT claimius.upsert_app_object(?, 'claimius.organization', ?, ?)`, linked.ID, home.Org, grantUser); err != nil {
		t.Fatalf("link the root to a second app: %v", err)
	}
	var owners uuid.UUID
	if err := prophet.NewRaw(`
		SELECT (claimius.insert_claim(p_name => 'Root owner', p_description => NULL, p_sa_access => 15,
		        p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?) -> 'claim' ->> 'id')::UUID
	`, home.Org, home.Org, grantUser).Scan(ctx, &owners); err != nil {
		t.Fatalf("owner claim: %v", err)
	}
	if _, err := prophet.ExecContext(ctx, `
		SELECT claimius.insert_claim_object(p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
		       p_sa_created_by => ?, p_object_id => ?, p_object_type => 'claimius.organization', p_inherits => TRUE)
	`, owners, home.Org, home.Org, grantUser, home.Org); err != nil {
		t.Fatalf("bind on the root: %v", err)
	}
	owner := uuid.New()
	if _, err := prophet.ExecContext(ctx, `
		SELECT claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Owner'),
		       claimius.insert_user_claim(p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?)
	`, owner, home.ID, owners, owner, home.Org, grantUser); err != nil {
		t.Fatalf("grant the owner: %v", err)
	}

	for _, app := range []peerApp{home, linked} {
		var access int
		if err := prophet.NewRaw(`SELECT sa_access FROM claimius.get_access(?, ?, 'claimius.organization', 1)`, owner, home.Org).Scan(ctx, &access); err != nil || access&1 == 0 {
			t.Fatalf("the owner claim gives owner on the root, got %d %v", access, err)
		}
		listed := make([]uuid.UUID, 0)
		if err := prophet.NewRaw(`SELECT id FROM claimius.get_organizations(?, ?, 1)`, owner, app.ID).Scan(ctx, &listed); err != nil {
			t.Fatalf("organizations in %s: %v", app.Slug, err)
		}
		if len(listed) != 1 || listed[0] != home.Org {
			t.Fatalf("the root is owned in every app it is linked to, %s lists %v", app.Slug, listed)
		}
	}
	listed := make([]uuid.UUID, 0)
	if err := prophet.NewRaw(`SELECT id FROM claimius.get_organizations(?, ?, 1)`, owner, unlinked.ID).Scan(ctx, &listed); err != nil || len(listed) != 0 {
		t.Fatalf("an app the root is not linked to lists no root, got %v %v", listed, err)
	}
}

func TestAFirstSignInGrantsTheDefaultClaimWithoutASecondUserRow(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	prophet := psdb.Instance()
	first := postPeerApp(t, grantUser)
	second := postPeerApp(t, grantUser)
	var member uuid.UUID
	if err := prophet.NewRaw(`
		UPDATE claimius.samna_app SET claim_id = (claimius.insert_claim(p_name => 'Second member', p_description => NULL, p_sa_access => 4,
		       p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?) -> 'claim' ->> 'id')::UUID
		WHERE id = ?
		RETURNING claim_id
	`, second.Org, second.Org, grantUser, second.ID).Scan(ctx, &member); err != nil {
		t.Fatalf("default claim of the second app: %v", err)
	}

	user := uuid.New()
	if _, err := prophet.ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Ada')`, user, first.ID); err != nil {
		t.Fatalf("sign in to the first app: %v", err)
	}
	for range 2 {
		if _, err := prophet.ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Other name')`, user, second.ID); err != nil {
			t.Fatalf("sign in to the second app: %v", err)
		}
	}

	var rows, grants int
	var name string
	if err := prophet.NewRaw(`
		SELECT count(*), min(first_name) FROM claimius.samna_user WHERE user_id = ? AND sa_deleted_at IS NULL
	`, user).Scan(ctx, &rows, &name); err != nil || rows != 1 || name != "Ada" {
		t.Fatalf("a sign in to a second app keeps the one row as it stands, got %d rows named %q: %v", rows, name, err)
	}
	if err := prophet.NewRaw(`
		SELECT count(*) FROM claimius.user_claim WHERE user_id = ? AND claim_id = ? AND sa_deleted_at IS NULL
	`, user, member).Scan(ctx, &grants); err != nil || grants != 1 {
		t.Fatalf("the first sign in grants the app's default claim once, got %d %v", grants, err)
	}
}

func TestInsertOrganizationUserWithoutAnIdentityCreatesOnce(t *testing.T) {
	_, claimID := getGrantFixture(t)
	ctx := context.Background()
	var caller uuid.UUID
	if err := psdb.Instance().NewRaw(`SELECT id FROM claimius.user_claim WHERE claim_id = ? AND user_id = ? AND sa_deleted_at IS NULL`, claimID, grantUser).Scan(ctx, &caller); err != nil {
		t.Fatalf("caller grant: %v", err)
	}
	var created, noIdentity bool
	var userID uuid.UUID
	if err := psdb.Instance().NewRaw(`
		SELECT (r ->> 'created')::BOOLEAN, (r -> 'samna_user' ->> 'user_id')::UUID, jsonb_typeof(r -> 'user_identity') = 'null'
		FROM claimius.insert_organization_user(?, ?, jsonb_build_object('claim_id', ?::TEXT, 'first_name', 'Nobody'), NULL) AS r
	`, caller, grantRoot, claimID).Scan(ctx, &created, &userID, &noIdentity); err != nil {
		t.Fatalf("insert_organization_user without an identity inserts the user with a new id: %v", err)
	}
	if !created || userID == uuid.Nil || !noIdentity {
		t.Fatalf("a user posted without an identity is created under a new id and no user_identity, got created %v id %s identity written %v", created, userID, !noIdentity)
	}
	_, err := psdb.Instance().ExecContext(ctx, `
		SELECT claimius.insert_organization_user(?, ?, jsonb_build_object('claim_id', ?::TEXT, 'user_id', ?::TEXT, 'first_name', 'Nobody'), NULL)
	`, caller, grantRoot, claimID, userID)
	var pgErr pgdriver.Error
	if !errors.As(err, &pgErr) || pgErr.Field('C') != "23505" {
		t.Fatalf("insert_organization_user without an identity and without upsert refuses a live user_id with 23505, got %v", err)
	}
}
