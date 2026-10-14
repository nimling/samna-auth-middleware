//go:build dbtest

package shared_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	sdb "github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/uptrace/bun"
)

func orgNames(orgs []sdb.Organization) map[uuid.UUID]string {
	names := make(map[uuid.UUID]string, len(orgs))
	for _, o := range orgs {
		names[o.ID] = o.Name
	}
	return names
}

func TestGetOrganizationsManyNamesRowsWithoutAReader(t *testing.T) {
	db := database(t)
	ctx := context.Background()

	orgs, err := db.GetOrganizationsMany(ctx, []uuid.UUID{orgAlpha, orgDropped}, nil, nil)
	if err != nil {
		t.Fatalf("by ids: %v", err)
	}
	names := orgNames(orgs)
	if len(orgs) != 2 || names[orgAlpha] != "Alpha" || names[orgDropped] != "Dropped" {
		t.Fatalf("expected Alpha and the soft deleted Dropped, got %+v", names)
	}
	for _, o := range orgs {
		if o.ID == orgDropped && o.DeletedAt == nil {
			t.Fatal("a soft deleted organization must come back with its sa_deleted_at")
		}
	}

	orgs, err = db.GetOrganizationsMany(ctx, nil, []uuid.UUID{orgRoot}, nil)
	if err != nil {
		t.Fatalf("by owners: %v", err)
	}
	names = orgNames(orgs)
	if names[orgRoot] != "Root" || names[orgAlpha] != "Alpha" {
		t.Fatalf("expected the self owned root and its child, got %+v", names)
	}
	for _, o := range orgs {
		if o.OwnerID != orgRoot {
			t.Fatalf("an organization owned elsewhere came back: %+v", o)
		}
	}

	orgs, err = db.GetOrganizationsMany(ctx, []uuid.UUID{orgAlpha, orgGrand}, []uuid.UUID{orgRoot}, nil)
	if err != nil {
		t.Fatalf("both lists: %v", err)
	}
	if len(orgs) != 1 || orgs[0].ID != orgAlpha {
		t.Fatalf("both lists must apply, got %+v", orgNames(orgs))
	}

	orgs, err = db.GetOrganizationsMany(ctx, nil, nil, nil)
	if err != nil {
		t.Fatalf("no lists: %v", err)
	}
	if len(orgs) != 0 {
		t.Fatalf("no list must give no rows, got %d", len(orgs))
	}
}

func TestGetLocationsManyWalksParents(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	root, child := uuid.New(), uuid.New()
	if _, err := db.NewRaw(`
		INSERT INTO claimius.location (id, name, sa_owner_id, sa_parent_id, sa_root_id, sa_created_by)
		VALUES (?, 'Floor', ?, ?, ?, ?)`, root, orgKept, root, root, userC).Exec(ctx); err != nil {
		t.Fatalf("insert root: %v", err)
	}
	if _, err := db.NewRaw(`
		INSERT INTO claimius.location (id, name, sa_owner_id, sa_parent_id, sa_root_id, sa_created_by, sa_deleted_at)
		VALUES (?, 'Room', ?, ?, ?, ?, now())`, child, orgKept, root, root, userC).Exec(ctx); err != nil {
		t.Fatalf("insert child: %v", err)
	}

	locs, err := db.GetLocationsMany(ctx, nil, []uuid.UUID{root}, nil)
	if err != nil {
		t.Fatalf("by parents: %v", err)
	}
	if len(locs) != 2 {
		t.Fatalf("expected the root and its soft deleted child, got %d", len(locs))
	}

	locs, err = db.GetLocationsMany(ctx, []uuid.UUID{child}, nil, nil)
	if err != nil {
		t.Fatalf("by ids: %v", err)
	}
	if len(locs) != 1 || locs[0].Name == nil || *locs[0].Name != "Room" || locs[0].DeletedAt == nil {
		t.Fatalf("expected the soft deleted Room, got %+v", locs)
	}
}

func TestGetUsersManyNamesUsersWithoutAReader(t *testing.T) {
	db := database(t)
	ctx := context.Background()

	users, err := db.GetUsersMany(ctx, []uuid.UUID{userA, userB}, nil, nil, nil)
	if err != nil {
		t.Fatalf("by user ids: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected two users, got %d", len(users))
	}

	var rowID uuid.UUID
	for _, u := range users {
		if u.UserID == userA {
			rowID = u.ID
		}
	}
	users, err = db.GetUsersMany(ctx, nil, []uuid.UUID{rowID}, nil, nil)
	if err != nil {
		t.Fatalf("by row ids: %v", err)
	}
	if len(users) != 1 || users[0].UserID != userA || users[0].FirstName == nil || *users[0].FirstName != "Ada" {
		t.Fatalf("expected Ada by row id, got %+v", users)
	}
}

func TestGetGrantsAndClaimsResolveCreatorsAndHolders(t *testing.T) {
	db := database(t)
	ctx := context.Background()

	held, err := db.GetGrants(ctx, sdb.GrantFilters{UserIDs: []uuid.UUID{userA}})
	if err != nil {
		t.Fatalf("by users: %v", err)
	}
	if len(held) < 2 {
		t.Fatalf("expected the two seeded grants of A, got %d", len(held))
	}

	creator, err := db.GetGrants(ctx, sdb.GrantFilters{IDs: []uuid.UUID{held[0].ID}})
	if err != nil {
		t.Fatalf("by ids: %v", err)
	}
	if len(creator) != 1 || creator[0].UserID != userA {
		t.Fatalf("a grant id must name its holder, got %+v", creator)
	}

	owned, err := db.GetGrants(ctx, sdb.GrantFilters{OwnerIDs: []uuid.UUID{orgKept}})
	if err != nil {
		t.Fatalf("by owners: %v", err)
	}
	holders := map[uuid.UUID]bool{}
	claimIDs := make([]uuid.UUID, 0, len(owned))
	for _, g := range owned {
		holders[g.UserID] = true
		claimIDs = append(claimIDs, g.ClaimID)
	}
	if !holders[userB] || !holders[userC] || holders[userA] {
		t.Fatalf("expected B and C to hold grants under Kept, got %+v", holders)
	}

	claims, err := db.GetClaimsMany(ctx, claimIDs, nil)
	if err != nil {
		t.Fatalf("claims by ids: %v", err)
	}
	names := map[string]bool{}
	for _, c := range claims {
		names[c.Name] = true
	}
	if !names["Kept owner"] || !names["Time report"] {
		t.Fatalf("expected the claims behind the grants, got %+v", names)
	}

	claims, err = db.GetClaimsMany(ctx, nil, []uuid.UUID{orgKept})
	if err != nil {
		t.Fatalf("claims by owners: %v", err)
	}
	if len(claims) < 3 {
		t.Fatalf("expected the claims Kept owns, got %d", len(claims))
	}

	var timeReport uuid.UUID
	for _, c := range claims {
		if c.Name == "Time report" {
			timeReport = c.ID
		}
	}
	byClaim, err := db.GetGrants(ctx, sdb.GrantFilters{ClaimIDs: []uuid.UUID{timeReport}, UserIDs: []uuid.UUID{userB}})
	if err != nil {
		t.Fatalf("by claim and user: %v", err)
	}
	if len(byClaim) != 1 || byClaim[0].UserID != userB {
		t.Fatalf("both lists must apply, got %+v", byClaim)
	}

	none, err := db.GetGrants(ctx, sdb.GrantFilters{})
	if err != nil {
		t.Fatalf("no lists: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("no list must give no rows, got %d", len(none))
	}
}

func TestGetGrantsLeavesOutClients(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	clientID := uuid.New()
	if _, err := db.NewRaw(`
		INSERT INTO claimius.samna_client (id, name, secret_hash, sa_owner_id)
		VALUES (?, 'Reads client', 'x', ?)`, clientID, orgKept).Exec(ctx); err != nil {
		t.Fatalf("insert client: %v", err)
	}
	var claimID uuid.UUID
	if err := db.NewRaw(`SELECT id FROM claimius.claim WHERE name = 'Kept owner' AND sa_deleted_at IS NULL`).Scan(ctx, &claimID); err != nil {
		t.Fatalf("kept owner claim: %v", err)
	}
	if _, err := db.NewRaw(`SELECT claimius.insert_user_claim(p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?)`,
		claimID, clientID, orgKept, userC).Exec(ctx); err != nil {
		t.Fatalf("grant client: %v", err)
	}

	all, err := db.GetGrants(ctx, sdb.GrantFilters{ClaimIDs: []uuid.UUID{claimID}})
	if err != nil {
		t.Fatalf("with clients: %v", err)
	}
	users, err := db.GetGrants(ctx, sdb.GrantFilters{ClaimIDs: []uuid.UUID{claimID}, NoClients: true})
	if err != nil {
		t.Fatalf("without clients: %v", err)
	}
	if len(all) != len(users)+1 {
		t.Fatalf("expected the client grant only in the first set, got %d and %d", len(all), len(users))
	}
	for _, g := range users {
		if g.UserID == clientID {
			t.Fatal("a client grant must be left out")
		}
	}
}

func TestGetBindingsOnObjectsAndClaims(t *testing.T) {
	db := database(t)
	ctx := context.Background()

	onKept, err := db.GetBindings(ctx, sdb.BindingFilters{ObjectType: sdb.OBJECT_TYPE_ORGANIZATION, ObjectIDs: []uuid.UUID{orgKept}})
	if err != nil {
		t.Fatalf("on object: %v", err)
	}
	if len(onKept) == 0 {
		t.Fatal("expected the Kept owner binding")
	}
	for _, b := range onKept {
		if b.ObjectID == nil || *b.ObjectID != orgKept {
			t.Fatalf("a binding on another object came back: %+v", b)
		}
	}

	ofClaim, err := db.GetBindings(ctx, sdb.BindingFilters{ClaimIDs: []uuid.UUID{onKept[0].ClaimID}})
	if err != nil {
		t.Fatalf("of claim: %v", err)
	}
	if len(ofClaim) == 0 || ofClaim[0].ClaimID != onKept[0].ClaimID {
		t.Fatalf("expected the bindings of the claim, got %+v", ofClaim)
	}

	ofType, err := db.GetBindings(ctx, sdb.BindingFilters{ObjectType: sdb.OBJECT_TYPE_ORGANIZATION})
	if err != nil {
		t.Fatalf("of type: %v", err)
	}
	if len(ofType) < 4 {
		t.Fatalf("expected the seeded organization bindings, got %d", len(ofType))
	}

	untyped, err := db.GetBindings(ctx, sdb.BindingFilters{ObjectIDs: []uuid.UUID{orgKept}})
	if err != nil {
		t.Fatalf("untyped: %v", err)
	}
	if len(untyped) != 0 {
		t.Fatalf("ids without a type must give no rows, got %d", len(untyped))
	}
}

func TestGetUserTokensOfUsers(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	user := uuid.New()
	if _, err := db.NewRaw(`
		INSERT INTO claimius.user_token (user_id, external_id, sa_owner_id, sa_created_by)
		VALUES (?, ?, ?, ?)`, user, "reads-"+user.String(), orgKept, userC).Exec(ctx); err != nil {
		t.Fatalf("insert token: %v", err)
	}
	tokens, err := db.GetUserTokens(ctx, []uuid.UUID{user})
	if err != nil {
		t.Fatalf("tokens: %v", err)
	}
	if len(tokens) != 1 || tokens[0].CreatedBy == nil || *tokens[0].CreatedBy != userC {
		t.Fatalf("expected the token with its creator, got %+v", tokens)
	}
}

func TestGetObjectTypesListsRegisteredTables(t *testing.T) {
	db := database(t)
	types, err := db.GetObjectTypes(context.Background())
	if err != nil {
		t.Fatalf("types: %v", err)
	}
	found := false
	for _, ty := range types {
		if ty == sdb.OBJECT_TYPE_ORGANIZATION {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected claimius.organization among %v", types)
	}
}

func TestGetObjectUsageNamesAndCountsTheReadersPairs(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	missing := uuid.New()

	rows, err := db.GetObjectUsage(ctx, userA, []uuid.UUID{orgRoot, missing}, []string{string(sdb.OBJECT_TYPE_ORGANIZATION), string(sdb.OBJECT_TYPE_ORGANIZATION)})
	if err != nil {
		t.Fatalf("typed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected one row per object, got %d", len(rows))
	}
	for _, r := range rows {
		switch r.ObjectID {
		case orgRoot:
			if r.Name == nil || *r.Name != "Root" || r.UsedCount == nil {
				t.Fatalf("expected Root with the reader's count, got %+v", r)
			}
		case missing:
			if r.Name != nil || r.UsedCount != nil {
				t.Fatalf("an unknown object has no name and no count, got %+v", r)
			}
		}
	}

	rows, err = db.GetObjectUsage(ctx, userA, []uuid.UUID{orgRoot}, nil)
	if err != nil {
		t.Fatalf("untyped: %v", err)
	}
	if len(rows) != 1 || rows[0].ObjectType == nil || *rows[0].ObjectType != sdb.OBJECT_TYPE_ORGANIZATION || rows[0].UsedCount == nil {
		t.Fatalf("an untyped id takes the type of the reader's pair, got %+v", rows)
	}
}

func TestGetUsersManyResolvesEmailsAndExternalIDs(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	user := uuid.New()
	email := "Mixed." + user.String() + "@Example.io"
	if _, err := db.NewRaw(`SELECT claimius.upsert_app_user(p_user_id => ?, p_first_name => 'Eve', p_email => ?)`, user, email).Exec(ctx); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	users, err := db.GetUsersMany(ctx, nil, nil, []string{strings.ToLower(email)}, nil)
	if err != nil {
		t.Fatalf("by email: %v", err)
	}
	if len(users) != 1 || users[0].UserID != user {
		t.Fatalf("an email must resolve without case, got %+v", users)
	}

	users, err = db.GetUsersMany(ctx, nil, nil, nil, []string{"ext-b"})
	if err != nil {
		t.Fatalf("by external id: %v", err)
	}
	if len(users) != 1 || users[0].UserID != userB {
		t.Fatalf("expected Bo by external id, got %+v", users)
	}

	users, err = db.GetUsersMany(ctx, []uuid.UUID{userA}, nil, nil, []string{"ext-b"})
	if err != nil {
		t.Fatalf("both lists: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("every given list must apply, got %+v", users)
	}
}

func TestGetOrganizationsAndLocationsManyResolveExternalIDs(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	org, loc := uuid.New(), uuid.New()
	orgExt, locExt := "org-"+org.String(), "loc-"+loc.String()
	if _, err := db.NewRaw(`
		INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by, external_id, sa_deleted_at)
		VALUES (?, 'External', 'company', ?, ?, 1, ?, ?, now())`, org, orgKept, orgKept, userC, orgExt).Exec(ctx); err != nil {
		t.Fatalf("insert organization: %v", err)
	}
	if _, err := db.NewRaw(`
		INSERT INTO claimius.location (id, name, sa_owner_id, sa_parent_id, sa_root_id, sa_created_by, external_id)
		VALUES (?, 'Lobby', ?, ?, ?, ?, ?)`, loc, orgKept, loc, loc, userC, locExt).Exec(ctx); err != nil {
		t.Fatalf("insert location: %v", err)
	}

	orgs, err := db.GetOrganizationsMany(ctx, nil, nil, []string{orgExt})
	if err != nil {
		t.Fatalf("organization by external id: %v", err)
	}
	if len(orgs) != 1 || orgs[0].ID != org || orgs[0].DeletedAt == nil {
		t.Fatalf("expected the soft deleted External, got %+v", orgNames(orgs))
	}

	orgs, err = db.GetOrganizationsMany(ctx, nil, []uuid.UUID{orgRoot}, []string{orgExt})
	if err != nil {
		t.Fatalf("organization by owner and external id: %v", err)
	}
	if len(orgs) != 0 {
		t.Fatalf("every given list must apply, got %+v", orgNames(orgs))
	}

	locs, err := db.GetLocationsMany(ctx, nil, nil, []string{locExt})
	if err != nil {
		t.Fatalf("location by external id: %v", err)
	}
	if len(locs) != 1 || locs[0].ID != loc {
		t.Fatalf("expected Lobby, got %+v", locs)
	}
}

func TestGetRelationsReadsTheReadersLiveRows(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	live, dropped, other := uuid.New(), uuid.New(), uuid.New()
	if _, err := db.NewRaw(`
		INSERT INTO claimius.user_relation (id, user_id, object_type, object_id, pinned, sa_deleted_at) VALUES
		    (?, ?, 'claimius.organization', ?, 1, NULL),
		    (?, ?, 'claimius.organization', ?, 1, now()),
		    (?, ?, 'claimius.organization', ?, 1, NULL)`,
		live, userA, orgAlpha, dropped, userA, orgGrand, other, userB, orgAlpha).Exec(ctx); err != nil {
		t.Fatalf("insert relations: %v", err)
	}

	relations, err := db.GetUserRelations(ctx, userA)
	if err != nil {
		t.Fatalf("relations: %v", err)
	}
	ids := map[uuid.UUID]bool{}
	for _, r := range relations {
		ids[r.ID] = true
		if r.UserID != userA {
			t.Fatalf("a relation of another user came back: %+v", r)
		}
	}
	if !ids[live] || ids[dropped] || ids[other] {
		t.Fatalf("expected only the live relation of A, got %+v", ids)
	}

	filtered := make([]sdb.UserRelation, 0)
	if err := db.GetUserRelationsQuery(&filtered, userA, sdb.UserRelationFilters{OrgIDs: []uuid.UUID{orgGrand}}).Scan(ctx); err != nil {
		t.Fatalf("filtered: %v", err)
	}
	if len(filtered) != 0 {
		t.Fatalf("a soft deleted relation must not pass a filter, got %+v", filtered)
	}

	relation, err := db.GetRelation(ctx, userA, live)
	if err != nil || relation.ID != live {
		t.Fatalf("expected the live relation by id, got %+v, %v", relation, err)
	}
	for _, id := range []uuid.UUID{dropped, other} {
		if _, err := db.GetRelation(ctx, userA, id); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("relation %s must not be read by A, got %v", id, err)
		}
	}
}

func TestGetAuditReadsOneRowByID(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	var reached, hidden uuid.UUID
	if err := db.NewRaw(`SELECT (claimius.insert_audit('update', 'test', 'claimius.organization', ?, ?, NULL, 'reached')).id`, orgRoot, orgRoot).Scan(ctx, &reached); err != nil {
		t.Fatalf("insert reached: %v", err)
	}
	if err := db.NewRaw(`SELECT (claimius.insert_audit('update', 'test', 'claimius.organization', ?, ?, NULL, 'hidden')).id`, orgKept, orgKept).Scan(ctx, &hidden); err != nil {
		t.Fatalf("insert hidden: %v", err)
	}

	audits, err := db.GetAudit(ctx, userA, sdb.ACCESS_READ, []uuid.UUID{reached})
	if err != nil {
		t.Fatalf("by id: %v", err)
	}
	if len(audits) != 1 || audits[0].ID != reached {
		t.Fatalf("expected the one row named, got %+v", audits)
	}

	audits, err = db.GetAudit(ctx, userA, sdb.ACCESS_READ, []uuid.UUID{hidden})
	if err != nil {
		t.Fatalf("hidden by id: %v", err)
	}
	if len(audits) != 0 {
		t.Fatalf("a row under an organization the reader does not reach must not come back, got %+v", audits)
	}

	audits, err = db.GetAudit(ctx, userA, sdb.ACCESS_READ, nil)
	if err != nil {
		t.Fatalf("without ids: %v", err)
	}
	if len(audits) == 0 {
		t.Fatal("without ids every reached row comes back")
	}
}

func TestGetBindingsCarryEffectiveAccess(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	bindings, err := db.GetBindings(ctx, sdb.BindingFilters{ObjectType: sdb.OBJECT_TYPE_ORGANIZATION})
	if err != nil {
		t.Fatalf("bindings: %v", err)
	}
	if len(bindings) == 0 {
		t.Fatal("expected the seeded organization bindings")
	}
	for _, b := range bindings {
		var want sdb.ClaimAccess
		if err := db.NewRaw(`SELECT claimius._effective_access(c.sa_access, b.sa_access) FROM claimius.claim_object b JOIN claimius.claim c ON c.id = b.claim_id WHERE b.id = ?`, b.ID).Scan(ctx, &want); err != nil {
			t.Fatalf("expected mask: %v", err)
		}
		if b.EffectiveAccess == nil || *b.EffectiveAccess != want {
			t.Fatalf("binding %s: expected effective access %d, got %v", b.ID, want, b.EffectiveAccess)
		}
	}
}

func TestGetDescendantsWalksWithoutAType(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	rows, err := db.GetDescendants(ctx, orgRoot)
	if err != nil {
		t.Fatalf("descendants: %v", err)
	}
	var typed []sdb.Descendant
	if err := db.NewRaw(`SELECT * FROM claimius.get_descendants(?, ?)`, sdb.OBJECT_TYPE_ORGANIZATION, orgRoot).Scan(ctx, &typed); err != nil {
		t.Fatalf("typed: %v", err)
	}
	key := func(d sdb.Descendant) string { return d.TreeType + string(d.ObjectType) + d.ObjectID.String() }
	want := map[string]int{}
	for _, d := range typed {
		want[key(d)] = d.Hop
	}
	got := map[string]int{}
	for _, d := range rows {
		got[key(d)] = d.Hop
	}
	if len(got) != len(want) {
		t.Fatalf("expected the typed walk, got %d rows for %d", len(got), len(want))
	}
	for k, hop := range want {
		if h, ok := got[k]; !ok || h != hop {
			t.Fatalf("expected %s at hop %d, got %d, %v", k, hop, h, ok)
		}
	}
	grand := false
	for _, d := range rows {
		if d.ObjectID == orgGrand && d.TreeType == "ownership" && d.Hop == 2 {
			grand = true
		}
	}
	if !grand {
		t.Fatalf("expected Grand two hops down the ownership tree, got %+v", rows)
	}
}

func TestModelsReadThroughTheGetViews(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	grants := make([]sdb.UserClaim, 0)
	q := db.NewSelect().Model(&grants).Relation("Owner").Relation("User").Where("user_claim.user_id = ?", userC)
	query := q.String()
	for _, name := range []string{`"claimius"."get_grants"`, `"claimius"."get_organizations"`, `"claimius"."get_users"`} {
		if !strings.Contains(query, name) {
			t.Fatalf("expected %s in %s", name, query)
		}
	}
	for _, name := range []string{`"claimius"."user_claim"`, `"claimius"."organization"`, `"claimius"."samna_user"`} {
		if strings.Contains(query, name) {
			t.Fatalf("a select must not name the table %s: %s", name, query)
		}
	}
	if err := q.Scan(ctx); err != nil {
		t.Fatalf("scan: %v", err)
	}
	dropped := false
	for _, g := range grants {
		if g.User == nil || g.User.FirstName == nil || *g.User.FirstName != "Cy" {
			t.Fatalf("expected Cy joined on every grant, got %+v", g.User)
		}
		if g.Owner != nil && g.Owner.ID == orgDropped {
			dropped = g.Owner.DeletedAt != nil
		}
	}
	if !dropped {
		t.Fatal("a soft deleted owner must join as it stands")
	}

	err := db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, `SET LOCAL ROLE claimius_reader`); err != nil {
			return err
		}
		for _, view := range []string{"get_users", "get_organizations", "get_locations", "get_claims", "get_grants"} {
			if _, err := tx.ExecContext(ctx, `SELECT count(*) FROM claimius.`+view); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("claimius_reader must select the views: %v", err)
	}
}
