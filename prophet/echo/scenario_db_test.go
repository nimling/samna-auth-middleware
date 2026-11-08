//go:build dbtest

package echo_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/disciple/sam"
	psam "github.com/nimling/samna-auth-middleware/v2/prophet/sam"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/smw"
)

func getDiscipleDB(t *testing.T) *shared.DB {
	t.Helper()
	for _, key := range []string{"PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD"} {
		value := os.Getenv("DISCIPLE_" + key)
		if value == "" {
			t.Fatalf("DISCIPLE_%s is not set: a disciple database is required, as just test and just verify provide", key)
		}
		t.Setenv(key, value)
	}
	db, err := shared.Connect(echo.New().Logger)
	if err != nil {
		t.Fatalf("connect disciple: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type scenarioClaim struct {
	name   string
	access int
	level  int
	bound  string
}

type scenarioUser struct {
	id     uuid.UUID
	name   string
	claims []string
	reads  bool
	level  int
}

func postScenarioClaims(t *testing.T, db *shared.DB, app peerApp, child, linker uuid.UUID, claims []scenarioClaim) map[string]uuid.UUID {
	t.Helper()
	ctx := context.Background()
	ids := make(map[string]uuid.UUID, len(claims))
	for _, c := range claims {
		var id uuid.UUID
		if err := db.NewRaw(`
			SELECT (claimius.insert_claim(p_app_id => ?, p_name => ?, p_description => NULL, p_sa_access => ?,
			        p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?, p_level => ?) -> 'claim' ->> 'id')::UUID
		`, app.ID, c.name, c.access, app.Org, app.Org, grantUser, c.level).Scan(ctx, &id); err != nil {
			t.Fatalf("claim %s: %v", c.name, err)
		}
		ids[c.name] = id
	}
	for _, c := range claims {
		objectID, objectType := app.Org, "claimius.organization"
		actor := grantUser
		switch c.bound {
		case "child":
			objectID = child
		case "":
			continue
		case "root":
		default:
			objectID, objectType = ids[c.bound], "claimius.claim"
			actor = linker
		}
		if _, err := db.ExecContext(ctx, `
			SELECT claimius.insert_claim_object(p_app_id => ?, p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
			       p_sa_created_by => ?, p_object_id => ?, p_object_type => ?, p_inherits => TRUE)
		`, app.ID, ids[c.name], app.Org, app.Org, actor, objectID, objectType); err != nil {
			t.Fatalf("bind %s: %v", c.name, err)
		}
	}
	return ids
}

func getScenarioAccess(t *testing.T, prophet, disciple *shared.DB, appID, child uuid.UUID, user scenarioUser) {
	t.Helper()
	ctx := context.Background()
	var onProphet []struct {
		Level int `bun:"level"`
	}
	if err := prophet.NewRaw(`SELECT level FROM claimius.get_access(?, ?, ?, 'claimius.organization', 4)`, user.id, appID, child).
		Scan(ctx, &onProphet); err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("%s on the prophet: %v", user.name, err)
	}
	ac, err := disciple.MutationAccess(ctx, user.id, child, shared.OBJECT_TYPE_ORGANIZATION, shared.ACCESS_READ)
	if err != nil && !errors.Is(err, shared.ErrNoAccess) {
		t.Fatalf("%s on the disciple: %v", user.name, err)
	}
	if (len(onProphet) > 0) != user.reads {
		t.Errorf("%s: the prophet answers read %v, want %v", user.name, len(onProphet) > 0, user.reads)
	}
	if (ac != nil) != user.reads {
		t.Errorf("%s: the disciple answers read %v, want %v", user.name, ac != nil, user.reads)
	}
	if user.reads && len(onProphet) > 0 && ac != nil && (onProphet[0].Level != user.level || ac.Level != user.level) {
		t.Errorf("%s: read at level %d on the prophet and %d on the disciple, want %d", user.name, onProphet[0].Level, ac.Level, user.level)
	}
}

func getScenarioListing(t *testing.T, prophet, disciple *shared.DB, appID uuid.UUID, user scenarioUser, step string) {
	t.Helper()
	ctx := context.Background()
	var onProphet, onDisciple []uuid.UUID
	if err := prophet.NewRaw(`SELECT object_id FROM claimius.get_objects(?, ?, 'claimius.organization', 4) ORDER BY object_id`, user.id, appID).
		Scan(ctx, &onProphet); err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("%s: list on the prophet: %v", user.name, err)
	}
	if err := disciple.NewRaw(`SELECT object_id FROM claimius.get_objects(?, 'claimius.organization', 4) ORDER BY object_id`, user.id).
		Scan(ctx, &onDisciple); err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("%s: list on the disciple: %v", user.name, err)
	}
	if fmt.Sprint(onProphet) != fmt.Sprint(onDisciple) {
		t.Errorf("%s %s: the prophet lists %v and the disciple %v", user.name, step, onProphet, onDisciple)
	}
}

func getScenarioUsers(t *testing.T, prophet, disciple *shared.DB, appID uuid.UUID, user scenarioUser, step string) {
	t.Helper()
	ctx := context.Background()
	var seenProphet, seenDisciple []uuid.UUID
	if err := prophet.NewRaw(`SELECT user_id FROM claimius.get_users(?, ?) ORDER BY user_id`, user.id, appID).
		Scan(ctx, &seenProphet); err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("%s: users on the prophet: %v", user.name, err)
	}
	if err := disciple.NewRaw(`SELECT user_id FROM claimius.get_users(?) ORDER BY user_id`, user.id).
		Scan(ctx, &seenDisciple); err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("%s: users on the disciple: %v", user.name, err)
	}
	if fmt.Sprint(seenProphet) != fmt.Sprint(seenDisciple) {
		t.Errorf("%s %s: the prophet sees users %v and the disciple %v", user.name, step, seenProphet, seenDisciple)
	}
}

func getDisciplePairCounts(t *testing.T, disciple *shared.DB, step string) {
	t.Helper()
	ctx := context.Background()
	if _, err := disciple.ExecContext(ctx, `SELECT claimius.drain_access_diff(1000)`); err != nil {
		t.Fatalf("drain on the disciple %s: %v", step, err)
	}
	var left, drift int
	if err := disciple.NewRaw(`SELECT count(*) FROM claimius.user_users_diff`).Scan(ctx, &left); err != nil {
		t.Fatalf("user_users_diff on the disciple: %v", err)
	}
	if left != 0 {
		t.Errorf("%s: the drain on the disciple left %d user_users_diff rows", step, left)
	}
	if err := disciple.NewRaw(`
		SELECT count(*) FROM (
			(SELECT a.user_id, b.user_id, count(*)::INTEGER
			 FROM claimius.user_object a
			     JOIN claimius.user_object b
			         ON b.object_type = a.object_type AND b.object_id = a.object_id AND b.user_id <> a.user_id
			 WHERE a.object_type <> 'claimius.claim'
			 GROUP BY a.user_id, b.user_id
			 EXCEPT
			 SELECT viewer_id, target_user_id, sharing_object_count FROM claimius.user_users)
			UNION ALL
			(SELECT viewer_id, target_user_id, sharing_object_count FROM claimius.user_users
			 EXCEPT
			 SELECT a.user_id, b.user_id, count(*)::INTEGER
			 FROM claimius.user_object a
			     JOIN claimius.user_object b
			         ON b.object_type = a.object_type AND b.object_id = a.object_id AND b.user_id <> a.user_id
			 WHERE a.object_type <> 'claimius.claim'
			 GROUP BY a.user_id, b.user_id)
		) d
	`).Scan(ctx, &drift); err != nil {
		t.Fatalf("user_users on the disciple: %v", err)
	}
	if drift != 0 {
		t.Errorf("%s: %d user_users rows on the disciple differ from a count over its pairs", step, drift)
	}
}

func TestEveryScenarioResolvesTheSameOnTheProphetAndItsDisciple(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	prophet := psdb.Instance()
	app := postPeerApp(t, grantUser)
	child := uuid.New()
	if _, err := prophet.ExecContext(ctx, `
		INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
		VALUES (?, 'Scenario child', 'company', ?, ?, 1, ?)
	`, child, app.Org, app.Org, grantUser); err != nil {
		t.Fatalf("child: %v", err)
	}
	if _, err := prophet.ExecContext(ctx, `SELECT claimius.upsert_app_organization(?, ?, TRUE, ?)`, app.ID, app.Org, grantUser); err != nil {
		t.Fatalf("link tree: %v", err)
	}

	admin := uuid.New()
	var adminClaim, linker uuid.UUID
	if err := prophet.NewRaw(`
		SELECT (claimius.insert_claim(p_app_id => ?, p_name => 'Scenario admin', p_description => NULL, p_sa_access => 15,
		        p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?, p_level => 10) -> 'claim' ->> 'id')::UUID
	`, app.ID, app.Org, app.Org, grantUser).Scan(ctx, &adminClaim); err != nil {
		t.Fatalf("admin claim: %v", err)
	}
	if _, err := prophet.ExecContext(ctx, `
		SELECT claimius.insert_claim_object(p_app_id => ?, p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
		       p_sa_created_by => ?, p_object_id => ?, p_object_type => 'claimius.organization', p_inherits => TRUE),
		       claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Admin')
	`, app.ID, adminClaim, app.Org, app.Org, grantUser, app.Org, admin, app.ID); err != nil {
		t.Fatalf("admin binding: %v", err)
	}
	if err := prophet.NewRaw(`
		SELECT (claimius.insert_user_claim(p_app_id => ?, p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?)
		        -> 'user_claim' ->> 'id')::UUID
	`, app.ID, adminClaim, admin, app.Org, grantUser).Scan(ctx, &linker); err != nil {
		t.Fatalf("admin grant: %v", err)
	}

	ids := postScenarioClaims(t, prophet, app, child, linker, []scenarioClaim{
		{name: "Inherited read", access: 4, bound: "root"},
		{name: "Read level 0", access: 4, bound: "child"},
		{name: "Read level 7", access: 4, level: 7, bound: "child"},
		{name: "Deny read level 0", access: 20, bound: "root"},
		{name: "Deny read level 5", access: 20, level: 5, bound: "root"},
		{name: "Linked level 0", access: 4, bound: "child"},
		{name: "Link level 6", access: 4, level: 6, bound: "Linked level 0"},
		{name: "Deny read level 9", access: 20, level: 9},
	})
	users := []scenarioUser{
		{name: "an inherited grant", claims: []string{"Inherited read"}, reads: true},
		{name: "a higher deny over a direct grant", claims: []string{"Read level 0", "Deny read level 5"}, reads: false},
		{name: "an equal deny under a direct grant", claims: []string{"Read level 0", "Deny read level 0"}, reads: true},
		{name: "a lower deny under a higher grant", claims: []string{"Read level 7", "Deny read level 5"}, reads: true, level: 7},
		{name: "a link at the higher level", claims: []string{"Link level 6", "Deny read level 5"}, reads: true, level: 6},
		{name: "no grant", reads: false},
	}
	for i := range users {
		users[i].id = uuid.New()
		if _, err := prophet.ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => ?)`,
			users[i].id, app.ID, users[i].name); err != nil {
			t.Fatalf("user %s: %v", users[i].name, err)
		}
		for _, name := range users[i].claims {
			if _, err := prophet.ExecContext(ctx, `
				SELECT claimius.insert_user_claim(p_app_id => ?, p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?)
			`, app.ID, ids[name], users[i].id, app.Org, grantUser); err != nil {
				t.Fatalf("grant %s to %s: %v", name, users[i].name, err)
			}
		}
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
	t.Setenv("SAUTH_ENDPOINT", server.URL)
	t.Setenv("SAUTH_APP_SLUG", app.Slug)
	t.Setenv("SAUTH_APP_SECRET", app.Secret)
	replica := &sam.Replica{DB: disciple}
	if err := replica.PullSnapshot(ctx, "", ""); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	for _, u := range users {
		getScenarioAccess(t, prophet, disciple, app.ID, child, u)
	}
	for _, u := range users {
		getScenarioListing(t, prophet, disciple, app.ID, u, "after the snapshot")
	}
	for _, u := range users {
		getScenarioUsers(t, prophet, disciple, app.ID, u, "after the snapshot")
	}
	getDisciplePairCounts(t, disciple, "after the snapshot")

	if _, err := prophet.ExecContext(ctx, `
		SELECT claimius.insert_claim_object(p_app_id => ?, p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
		       p_sa_created_by => ?, p_object_id => ?, p_object_type => 'claimius.organization', p_inherits => TRUE)
	`, app.ID, ids["Deny read level 9"], app.Org, app.Org, grantUser, child); err != nil {
		t.Fatalf("bind the level 9 deny: %v", err)
	}
	for _, i := range []int{3, 4} {
		if _, err := prophet.ExecContext(ctx, `
			SELECT claimius.insert_user_claim(p_app_id => ?, p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?)
		`, app.ID, ids["Deny read level 9"], users[i].id, app.Org, grantUser); err != nil {
			t.Fatalf("grant the level 9 deny: %v", err)
		}
		users[i].reads = false
	}
	if n, err := replica.PullSync(ctx); err != nil || n == 0 {
		t.Fatalf("the feed must bring the new deny, got %d %v", n, err)
	}
	for _, u := range users {
		getScenarioAccess(t, prophet, disciple, app.ID, child, u)
	}
	for _, u := range users {
		getScenarioListing(t, prophet, disciple, app.ID, u, "after the feed")
	}
	for _, u := range users {
		getScenarioUsers(t, prophet, disciple, app.ID, u, "after the feed")
	}
	getDisciplePairCounts(t, disciple, "after the feed")
}
