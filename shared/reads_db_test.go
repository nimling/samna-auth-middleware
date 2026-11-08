//go:build dbtest

package shared_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	sdb "github.com/nimling/samna-auth-middleware/v2/shared"
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

	orgs, err := db.GetOrganizationsMany(ctx, []uuid.UUID{orgAlpha, orgDropped}, nil)
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

	orgs, err = db.GetOrganizationsMany(ctx, nil, []uuid.UUID{orgRoot})
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

	orgs, err = db.GetOrganizationsMany(ctx, []uuid.UUID{orgAlpha, orgGrand}, []uuid.UUID{orgRoot})
	if err != nil {
		t.Fatalf("both lists: %v", err)
	}
	if len(orgs) != 1 || orgs[0].ID != orgAlpha {
		t.Fatalf("both lists must apply, got %+v", orgNames(orgs))
	}

	orgs, err = db.GetOrganizationsMany(ctx, nil, nil)
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

	locs, err := db.GetLocationsMany(ctx, nil, []uuid.UUID{root})
	if err != nil {
		t.Fatalf("by parents: %v", err)
	}
	if len(locs) != 2 {
		t.Fatalf("expected the root and its soft deleted child, got %d", len(locs))
	}

	locs, err = db.GetLocationsMany(ctx, []uuid.UUID{child}, nil)
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

	users, err := db.GetUsersMany(ctx, []uuid.UUID{userA, userB}, nil)
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
	users, err = db.GetUsersMany(ctx, nil, []uuid.UUID{rowID})
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
