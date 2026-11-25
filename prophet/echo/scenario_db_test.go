//go:build dbtest

package echo_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/disciple/sam"
	psam "github.com/nimling/samna-auth-middleware/v2/prophet/sam"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/smw"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

func getDiscipleDB(t *testing.T) *shared.DB {
	return getRoleDB(t, "DISCIPLE_", "a disciple database is required, as just test and just verify provide")
}

func getRoleDB(t *testing.T, prefix, need string) *shared.DB {
	t.Helper()
	env := make(map[string]string)
	for _, key := range []string{"PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD"} {
		value := os.Getenv(prefix + key)
		if value == "" {
			t.Fatalf("%s%s is not set: %s", prefix, key, need)
		}
		env[key] = value
	}
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", env["PGUSER"], env["PGPASSWORD"], env["PGHOST"], env["PGPORT"], env["PGDATABASE"])
	db := &shared.DB{DB: bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New()), Logger: echo.New().Logger, ConnStr: dsn}
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
			SELECT (claimius.insert_claim(p_name => ?, p_description => NULL, p_sa_access => ?,
			        p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?, p_level => ?) -> 'claim' ->> 'id')::UUID
		`, c.name, c.access, app.Org, app.Org, grantUser, c.level).Scan(ctx, &id); err != nil {
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
			SELECT claimius.insert_claim_object(p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
			       p_sa_created_by => ?, p_object_id => ?, p_object_type => ?, p_inherits => TRUE)
		`, ids[c.name], app.Org, app.Org, actor, objectID, objectType); err != nil {
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
	if err := prophet.NewRaw(`SELECT level FROM claimius.get_access(?, ?, 'claimius.organization', 4)`, user.id, child).
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
	if err := prophet.NewRaw(`SELECT object_id FROM claimius.get_objects(?, 'claimius.organization', 4) ORDER BY object_id`, user.id).
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
	if err := prophet.NewRaw(`SELECT user_id FROM claimius.get_users(?) ORDER BY user_id`, user.id).
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
	if _, err := disciple.ExecContext(ctx, `SELECT * FROM claimius.drain_access_diff(1000)`); err != nil {
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
			   AND a.user_id IN (SELECT v.user_id FROM claimius.user_activity v WHERE v.directory_at IS NOT NULL)
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
		VALUES (?, 'Scenario child', 'customer', ?, ?, 1, ?)
	`, child, app.Org, app.Org, grantUser); err != nil {
		t.Fatalf("child: %v", err)
	}
	if _, err := prophet.ExecContext(ctx, `SELECT claimius.upsert_app_organization(?, ?, TRUE, ?)`, app.ID, app.Org, grantUser); err != nil {
		t.Fatalf("link tree: %v", err)
	}

	admin := uuid.New()
	var adminClaim, linker uuid.UUID
	if err := prophet.NewRaw(`
		SELECT (claimius.insert_claim(p_name => 'Scenario admin', p_description => NULL, p_sa_access => 15,
		        p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?, p_level => 10) -> 'claim' ->> 'id')::UUID
	`, app.Org, app.Org, grantUser).Scan(ctx, &adminClaim); err != nil {
		t.Fatalf("admin claim: %v", err)
	}
	if _, err := prophet.ExecContext(ctx, `
		SELECT claimius.insert_claim_object(p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
		       p_sa_created_by => ?, p_object_id => ?, p_object_type => 'claimius.organization', p_inherits => TRUE),
		       claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Admin')
	`, adminClaim, app.Org, app.Org, grantUser, app.Org, admin, app.ID); err != nil {
		t.Fatalf("admin binding: %v", err)
	}
	if err := prophet.NewRaw(`
		SELECT (claimius.insert_user_claim(p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?)
		        -> 'user_claim' ->> 'id')::UUID
	`, adminClaim, admin, app.Org, grantUser).Scan(ctx, &linker); err != nil {
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
	if _, err := prophet.ExecContext(ctx, `
		UPDATE claimius.samna_app SET claim_id = (claimius.insert_claim(p_name => 'Scenario member', p_description => NULL, p_sa_access => 4,
		       p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?) -> 'claim' ->> 'id')::UUID
		WHERE id = ?
	`, app.Org, app.Org, grantUser, app.ID); err != nil {
		t.Fatalf("the app's default claim, granted on a first sign in: %v", err)
	}
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
				SELECT claimius.insert_user_claim(p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?)
			`, ids[name], users[i].id, app.Org, grantUser); err != nil {
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
	t.Cleanup(func() {
		if _, err := disciple.ExecContext(context.Background(), `UPDATE claimius.disciple_state SET prophet = NULL, endpoint = NULL`); err != nil {
			t.Errorf("unbind the disciple: %v", err)
		}
	})
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
		SELECT claimius.insert_claim_object(p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
		       p_sa_created_by => ?, p_object_id => ?, p_object_type => 'claimius.organization', p_inherits => TRUE)
	`, ids["Deny read level 9"], app.Org, app.Org, grantUser, child); err != nil {
		t.Fatalf("bind the level 9 deny: %v", err)
	}
	for _, i := range []int{3, 4} {
		if _, err := prophet.ExecContext(ctx, `
			SELECT claimius.insert_user_claim(p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?)
		`, ids["Deny read level 9"], users[i].id, app.Org, grantUser); err != nil {
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

func TestScenarioAMovedClaimAndANewGrantReachTheDisciple(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	prophet := psdb.Instance()
	app := postPeerApp(t, grantUser)
	other := postPeerApp(t, grantUser)
	team := uuid.New()
	if _, err := prophet.ExecContext(ctx, `
		INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
		VALUES (?, 'Scenario team', 'customer', ?, ?, 1, ?)
	`, team, app.Org, app.Org, grantUser); err != nil {
		t.Fatalf("team: %v", err)
	}
	if _, err := prophet.ExecContext(ctx, `SELECT claimius.upsert_app_organization(?, ?, TRUE, ?)`, app.ID, app.Org, grantUser); err != nil {
		t.Fatalf("link tree: %v", err)
	}
	if _, err := prophet.ExecContext(ctx, `SELECT claimius.upsert_app_object(?, 'claimius.organization', ?, ?)`, other.ID, app.Org, grantUser); err != nil {
		t.Fatalf("link the root to the other app: %v", err)
	}
	member := uuid.New()
	if _, err := prophet.ExecContext(ctx, `
		SELECT claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Member'),
		       claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Member')
	`, member, app.ID, member, other.ID); err != nil {
		t.Fatalf("member: %v", err)
	}

	for _, name := range []string{"Standing one", "Standing two", "Standing three"} {
		postPeerClaim(t, prophet, app.Org, grantUser, name)
	}
	moved := postPeerClaim(t, prophet, team, grantUser, "Moved")
	var oldBinding uuid.UUID
	if err := prophet.NewRaw(`
		SELECT (claimius.insert_claim_object(p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
		       p_sa_created_by => ?, p_object_id => ?, p_object_type => 'claimius.organization', p_inherits => TRUE) -> 'claim_object' ->> 'id')::UUID
	`, moved, team, app.Org, grantUser, team).Scan(ctx, &oldBinding); err != nil {
		t.Fatalf("bind on the team: %v", err)
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
	t.Cleanup(func() {
		if _, err := disciple.ExecContext(context.Background(), `UPDATE claimius.disciple_state SET prophet = NULL, endpoint = NULL`); err != nil {
			t.Errorf("unbind the disciple: %v", err)
		}
	})
	t.Setenv("SAUTH_ENDPOINT", server.URL)
	t.Setenv("SAUTH_APP_SLUG", app.Slug)
	t.Setenv("SAUTH_APP_SECRET", app.Secret)
	replica := &sam.Replica{DB: disciple}
	if err := replica.PullSnapshot(ctx, "", ""); err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	var newBinding, manager, scope uuid.UUID
	if err := prophet.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE claimius.claim SET sa_owner_id = ? WHERE id = ?`, app.Org, moved); err != nil {
			return fmt.Errorf("move owner: %w", err)
		}
		if err := tx.NewRaw(`
			SELECT (claimius.insert_claim_object(p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
			       p_sa_created_by => ?, p_object_id => ?, p_object_type => 'claimius.organization', p_inherits => TRUE) -> 'claim_object' ->> 'id')::UUID
		`, moved, app.Org, app.Org, grantUser, app.Org).Scan(ctx, &newBinding); err != nil {
			return fmt.Errorf("rebind: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `SELECT * FROM claimius.delete_claim_object(?, ?)`, oldBinding, grantUser); err != nil {
			return fmt.Errorf("unbind the team: %w", err)
		}
		if err := tx.NewRaw(`
			SELECT (claimius.insert_claim(p_name => 'Employee Manager', p_description => NULL, p_sa_access => 14,
			        p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?) -> 'claim' ->> 'id')::UUID
		`, app.Org, app.Org, grantUser).Scan(ctx, &manager); err != nil {
			return fmt.Errorf("manager: %w", err)
		}
		if err := tx.NewRaw(`
			SELECT (claimius.insert_claim_object(p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
			       p_sa_created_by => ?, p_sa_access => 6, p_scope => '{"employee": {"_access": "READ|WRITE"}}'::JSONB) -> 'claim_object' ->> 'id')::UUID
		`, manager, app.Org, app.Org, grantUser).Scan(ctx, &scope); err != nil {
			return fmt.Errorf("manager scope: %w", err)
		}
		_, err := tx.ExecContext(ctx, `
			SELECT claimius.insert_user_claim(p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?)
		`, manager, member, app.Org, grantUser)
		return err
	}); err != nil {
		t.Fatalf("move, rebind and the new claim: %v", err)
	}
	if n, err := replica.PullSync(ctx); err != nil || n == 0 {
		t.Fatalf("the feed must bring the move, the rebind and the new claim, got %d %v", n, err)
	}
	var owner uuid.UUID
	if err := disciple.NewRaw(`SELECT sa_owner_id FROM claimius.claim WHERE id = ?`, moved).Scan(ctx, &owner); err != nil || owner != app.Org {
		t.Errorf("the moved claim is owned by %s on the disciple, want %s: %v", owner, app.Org, err)
	}
	var live int
	if err := disciple.NewRaw(`
		SELECT count(*) FROM claimius.claim_object WHERE id IN (?, ?) AND sa_deleted_at IS NULL
	`, newBinding, scope).Scan(ctx, &live); err != nil || live != 2 {
		t.Errorf("the new binding and the scope binding are live on the disciple %d times, want 2: %v", live, err)
	}
	var granted bool
	if err := disciple.NewRaw(`
		SELECT EXISTS (SELECT 1 FROM claimius.user_claim WHERE claim_id = ? AND user_id = ? AND sa_deleted_at IS NULL)
	`, manager, member).Scan(ctx, &granted); err != nil || !granted {
		t.Errorf("the grant after the move must reach the disciple: %v", err)
	}
}

func getScenarioReplica(t *testing.T, app peerApp) (*sam.Replica, *shared.DB) {
	t.Helper()
	ctx := context.Background()
	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("request_id", uuid.New())
			return next(c)
		}
	})
	psam.RegisterEndpoints(e.Group("/sam", smw.AppMiddleware))
	server := httptest.NewServer(e)
	t.Cleanup(server.Close)

	disciple := getDiscipleDB(t)
	if _, err := disciple.ExecContext(ctx, `SELECT claimius.upsert_disciple_state(?)`, app.Slug); err != nil {
		t.Fatalf("disciple state: %v", err)
	}
	t.Cleanup(func() {
		if _, err := disciple.ExecContext(context.Background(), `UPDATE claimius.disciple_state SET prophet = NULL, endpoint = NULL`); err != nil {
			t.Errorf("unbind the disciple: %v", err)
		}
	})
	t.Setenv("SAUTH_ENDPOINT", server.URL)
	t.Setenv("SAUTH_APP_SLUG", app.Slug)
	t.Setenv("SAUTH_APP_SECRET", app.Secret)
	replica := &sam.Replica{DB: disciple}
	if err := replica.PullSnapshot(ctx, "", ""); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return replica, disciple
}

func postScenarioRoot(t *testing.T, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := psdb.Instance().ExecContext(context.Background(), `
		INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
		VALUES (?, ?, 'customer', ?, ?, 0, ?)
	`, id, name, id, id, grantUser); err != nil {
		t.Fatalf("root %s: %v", name, err)
	}
	return id
}

func postScenarioMember(t *testing.T, app peerApp) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := psdb.Instance().ExecContext(context.Background(),
		`SELECT claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Member')`, id, app.ID); err != nil {
		t.Fatalf("member: %v", err)
	}
	return id
}

func postScenarioGrant(t *testing.T, claim, user, owner uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := psdb.Instance().NewRaw(`
		SELECT (claimius.insert_user_claim(p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?)
		        -> 'user_claim' ->> 'id')::UUID
	`, claim, user, owner, grantUser).Scan(context.Background(), &id); err != nil {
		t.Fatalf("grant: %v", err)
	}
	return id
}

func getScenarioDrift(t *testing.T, prophet, disciple *shared.DB, table string, ids ...uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	var onProphet, onDisciple []string
	query := fmt.Sprintf(`SELECT id::TEXT || ' ' || coalesce(sa_deleted_at::TEXT, 'live') FROM claimius.%s WHERE id IN (?) ORDER BY id`, table)
	if err := prophet.NewRaw(query, bun.In(ids)).Scan(ctx, &onProphet); err != nil {
		t.Fatalf("%s on the prophet: %v", table, err)
	}
	if err := disciple.NewRaw(query, bun.In(ids)).Scan(ctx, &onDisciple); err != nil {
		t.Fatalf("%s on the disciple: %v", table, err)
	}
	if len(onProphet) != len(ids) || fmt.Sprint(onProphet) != fmt.Sprint(onDisciple) {
		t.Errorf("%s: the prophet holds %v and the disciple %v, want the same %d rows", table, onProphet, onDisciple, len(ids))
	}
}

func TestScenarioAGrantOfASoftDeletedClaimReachesALinkSeededReplica(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	prophet := psdb.Instance()
	app := postPeerApp(t, grantUser)
	side := postScenarioRoot(t, "Scenario side")
	member := postScenarioMember(t, app)
	gone := postPeerClaim(t, prophet, side, grantUser, "Gone")
	grant := postScenarioGrant(t, gone, member, side)
	if _, err := prophet.ExecContext(ctx, `UPDATE claimius.claim SET sa_deleted_at = now() WHERE id = ?`, gone); err != nil {
		t.Fatalf("soft delete the claim: %v", err)
	}

	replica, disciple := getScenarioReplica(t, app)
	if _, err := prophet.ExecContext(ctx, `SELECT claimius.upsert_app_object(?, 'claimius.organization', ?, ?)`, app.ID, side, grantUser); err != nil {
		t.Fatalf("link the side organization: %v", err)
	}
	if n, err := replica.PullSync(ctx); err != nil || n == 0 {
		t.Fatalf("the feed must bring the link, got %d %v", n, err)
	}
	getScenarioDrift(t, prophet, disciple, "claim", gone)

	if _, err := prophet.ExecContext(ctx, `UPDATE claimius.user_claim SET sa_deleted_at = now() WHERE id = ?`, grant); err != nil {
		t.Fatalf("revoke the grant: %v", err)
	}
	if n, err := replica.PullSync(ctx); err != nil || n == 0 {
		t.Fatalf("the feed must bring the revoked grant of the soft deleted claim, got %d %v", n, err)
	}
	getScenarioDrift(t, prophet, disciple, "claim", gone)
	getScenarioDrift(t, prophet, disciple, "user_claim", grant)
}

func TestScenarioAGrantBringsAClaimCommittedWhileItsLinkWasOpen(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	prophet := psdb.Instance()
	app := postPeerApp(t, grantUser)
	side := postScenarioRoot(t, "Scenario side")
	member := postScenarioMember(t, app)
	replica, disciple := getScenarioReplica(t, app)

	var missed uuid.UUID
	if err := prophet.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, `SELECT claimius.upsert_app_object(?, 'claimius.organization', ?, ?)`, app.ID, side, grantUser); err != nil {
			return fmt.Errorf("link the side organization: %w", err)
		}
		missed = postPeerClaim(t, prophet, side, grantUser, "Missed")
		return nil
	}); err != nil {
		t.Fatalf("link with a claim committed beside it: %v", err)
	}
	if n, err := replica.PullSync(ctx); err != nil || n == 0 {
		t.Fatalf("the feed must bring the link, got %d %v", n, err)
	}

	grant := postScenarioGrant(t, missed, member, side)
	if n, err := replica.PullSync(ctx); err != nil || n == 0 {
		t.Fatalf("the feed must bring the grant with its claim before it, got %d %v", n, err)
	}
	getScenarioDrift(t, prophet, disciple, "claim", missed)
	getScenarioDrift(t, prophet, disciple, "user_claim", grant)
}

func TestScenarioAGrantAndABindingOwnedOutsideTheAppReachTheDisciple(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	prophet := psdb.Instance()
	app := postPeerApp(t, grantUser)
	outside := postScenarioRoot(t, "Scenario outside")
	member := postScenarioMember(t, app)
	claim := postPeerClaim(t, prophet, app.Org, grantUser, "Owned inside")
	replica, disciple := getScenarioReplica(t, app)

	var binding uuid.UUID
	if err := prophet.NewRaw(`
		SELECT (claimius.insert_claim_object(p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
		       p_sa_created_by => ?, p_object_id => ?, p_object_type => 'claimius.organization', p_inherits => TRUE) -> 'claim_object' ->> 'id')::UUID
	`, claim, outside, outside, grantUser, app.Org).Scan(ctx, &binding); err != nil {
		t.Fatalf("bind with an owner outside the app: %v", err)
	}
	grant := postScenarioGrant(t, claim, member, outside)

	if n, err := replica.PullSync(ctx); err != nil || n == 0 {
		t.Fatalf("the feed must bring a binding and a grant whose owner the app is not linked to, got %d %v", n, err)
	}
	getScenarioDrift(t, prophet, disciple, "claim_object", binding)
	getScenarioDrift(t, prophet, disciple, "user_claim", grant)
	if ac, err := disciple.MutationAccess(ctx, member, app.Org, shared.OBJECT_TYPE_ORGANIZATION, shared.ACCESS_READ); err != nil || ac == nil {
		t.Errorf("the member reads the organization through the binding owned outside the app, got %+v %v", ac, err)
	}
	if claims, err := disciple.GetUserClaims(ctx, member); err != nil || len(claims) != 1 || claims[0].OwnerID != outside || claims[0].Owner != nil {
		t.Errorf("the grant is read with its owner id and no organization the disciple does not hold, got %+v %v", claims, err)
	}

	if _, err := disciple.ExecContext(ctx, `DELETE FROM claimius.claim_object WHERE id = ?; DELETE FROM claimius.user_claim WHERE id = ?`, binding, grant); err != nil {
		t.Fatalf("drop the rows to take them from the snapshot: %v", err)
	}
	if err := replica.PullSnapshot(ctx, "", "scenario"); err != nil {
		t.Fatalf("the snapshot must bring them too: %v", err)
	}
	getScenarioDrift(t, prophet, disciple, "claim_object", binding)
	getScenarioDrift(t, prophet, disciple, "user_claim", grant)
}

func TestScenarioAReplicaStuckOnAGrantWithoutItsClaimResumesFromTheSnapshot(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	prophet := psdb.Instance()
	app := postPeerApp(t, grantUser)
	side := postScenarioRoot(t, "Scenario side")
	member := postScenarioMember(t, app)
	gone := postPeerClaim(t, prophet, side, grantUser, "Gone")
	grant := postScenarioGrant(t, gone, member, side)
	if _, err := prophet.ExecContext(ctx, `UPDATE claimius.claim SET sa_deleted_at = now() WHERE id = ?`, gone); err != nil {
		t.Fatalf("soft delete the claim: %v", err)
	}
	replica, disciple := getScenarioReplica(t, app)

	if _, err := prophet.ExecContext(ctx, `
		SELECT claimius.upsert_app_object(?, 'claimius.organization', ?, ?);
		UPDATE claimius.user_claim SET sa_deleted_at = now() WHERE id = ?;
		DELETE FROM claimius.sync_event WHERE app_id = ? AND event_type = 'claimius.claim' AND payload ->> 'id' = ?
	`, app.ID, side, grantUser, grant, app.ID, gone.String()); err != nil {
		t.Fatalf("link, revoke and leave the outbox as it was written without the claim: %v", err)
	}
	if _, err := replica.PullSync(ctx); err == nil || !strings.Contains(err.Error(), "user_claim_claim_id_fkey") {
		t.Fatalf("a grant without its claim must hold the page, got %v", err)
	}
	stuck, err := replica.GetState(ctx)
	if err != nil {
		t.Fatalf("read the stuck cursor: %v", err)
	}

	for range 2 {
		if err := replica.PullSnapshot(ctx, "", "scenario"); err != nil {
			t.Fatalf("the snapshot must apply over the rows the replica holds: %v", err)
		}
	}
	if resumed, err := replica.GetState(ctx); err != nil || !stuck.Cursor.Less(resumed.Cursor) {
		t.Fatalf("the snapshot must move the cursor past %s, got %s %v", stuck.Cursor, resumed.Cursor, err)
	}
	getScenarioDrift(t, prophet, disciple, "claim", gone)
	getScenarioDrift(t, prophet, disciple, "user_claim", grant)

	kept := postPeerClaim(t, prophet, side, grantUser, "Kept")
	again := postScenarioGrant(t, kept, member, side)
	if n, err := replica.PullSync(ctx); err != nil || n == 0 {
		t.Fatalf("the feed must go on after the snapshot, got %d %v", n, err)
	}
	getScenarioDrift(t, prophet, disciple, "claim", gone, kept)
	getScenarioDrift(t, prophet, disciple, "user_claim", grant, again)
}
