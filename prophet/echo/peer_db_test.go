//go:build dbtest

package echo_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	sam "github.com/nimling/samna-auth-middleware/v2/disciple/sam"
	"github.com/nimling/samna-auth-middleware/v2/pkg/encryption"
	psam "github.com/nimling/samna-auth-middleware/v2/prophet/sam"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/smw"
)

// getPeerDB connects to the second prophet database just test keeps next to
// the first, named by the PEER_ prefixed libpq variables.
func getPeerDB(t *testing.T) *shared.DB {
	t.Helper()
	for _, key := range []string{"PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD"} {
		value := os.Getenv("PEER_" + key)
		if value == "" {
			t.Fatalf("PEER_%s is not set: a peer prophet database is required, as just test provides", key)
		}
		t.Setenv(key, value)
	}
	db, err := shared.Connect(echo.New().Logger)
	if err != nil {
		t.Fatalf("connect peer: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type peerApp struct {
	ID     uuid.UUID
	Org    uuid.UUID
	Slug   string
	Secret string
}

// postPeerApp registers on the home prophet an app with the key material its
// /sam app auth checks, owned by a root organization linked to it.
func postPeerApp(t *testing.T, actor uuid.UUID) peerApp {
	t.Helper()
	ctx := context.Background()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	orgSeed := make([]byte, 32)
	if _, err := rand.Read(orgSeed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	secret, hash, err := encryption.GenerateAppSecret(orgSeed, 1)
	if err != nil {
		t.Fatalf("app secret: %v", err)
	}
	app := peerApp{ID: uuid.New(), Org: uuid.New(), Slug: "gotest_peer_" + uuid.NewString()[:8], Secret: secret}
	db := psdb.Instance()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO claimius.organization (id, name, type, private_seed, sa_owner_id, sa_root_id, sa_level, sa_created_by)
		VALUES (?, 'Peer home', 'company', ?, ?, ?, 0, ?)
	`, app.Org, hex.EncodeToString(orgSeed), app.Org, app.Org, actor); err != nil {
		t.Fatalf("home org: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO claimius.samna_app (id, slug, name, private_key, private_seed, app_secret, sa_owner_id, sa_created_by)
		VALUES (?, ?, 'Peer app', ?, ?, ?, ?, ?)
	`, app.ID, app.Slug, base64.RawURLEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(key)), hex.EncodeToString(orgSeed), hash, app.Org, actor); err != nil {
		t.Fatalf("home app: %v", err)
	}
	if _, err := db.ExecContext(ctx, `SELECT claimius.link_app_object(?, 'claimius.organization', ?, ?)`, app.ID, app.Org, actor); err != nil {
		t.Fatalf("link org: %v", err)
	}
	return app
}

func postPeerClaim(t *testing.T, db *shared.DB, appID, org, actor uuid.UUID, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.NewRaw(`
		SELECT (claimius.create_claim(p_app_id => ?, p_name => ?, p_description => NULL, p_sa_access => 4,
		        p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?) -> 'claim' ->> 'id')::UUID
	`, appID, name, org, org, actor).Scan(context.Background(), &id); err != nil {
		t.Fatalf("claim %s: %v", name, err)
	}
	return id
}

func TestProphetMirrorsAPeersAppThroughItsFeed(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	var home string
	if err := psdb.Instance().NewRaw(`SELECT slug FROM claimius.get_app()`).Scan(ctx, &home); err != nil {
		t.Fatalf("home slug: %v", err)
	}
	app := postPeerApp(t, grantUser)
	first := postPeerClaim(t, psdb.Instance(), app.ID, app.Org, grantUser, "Mirrored first")

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

	mirror := getPeerDB(t)
	var actor uuid.UUID
	if err := mirror.NewRaw(`
		SELECT (claimius.init(p_system_app_slug => 'gotest_mirror', p_system_app_name => 'Go mirror',
		        p_system_app_private_key => 'key', p_system_app_private_seed => 'seed',
		        p_default_claim => '{"name": "Member", "description": "Default member claim"}'::JSONB) -> 'user_claim' ->> 'id')::UUID
	`).Scan(ctx, &actor); err != nil {
		t.Fatalf("init mirror: %v", err)
	}
	var peer shared.ProphetPeer
	if err := mirror.NewRaw(`
		INSERT INTO claimius.prophet_peer (prophet, endpoint, app_slug, app_secret, sa_created_by)
		VALUES (?, ?, ?, ?, ?) RETURNING *
	`, home, server.URL, app.Slug, app.Secret, actor).Scan(ctx, &peer); err != nil {
		t.Fatalf("peer row: %v", err)
	}
	replica := &sam.Replica{DB: mirror, Peer: &peer}

	if err := replica.PullSnapshot(ctx, "", ""); err != nil {
		t.Fatalf("snapshot from the peer: %v", err)
	}
	var mirrored struct {
		Home   string `bun:"home"`
		Linked bool   `bun:"linked"`
		Claim  bool   `bun:"claim"`
	}
	probe := func() {
		t.Helper()
		if err := mirror.NewRaw(`
			SELECT a.home_prophet AS home,
			       claimius.app_has_object(a.id, 'claimius.organization', ?) AS linked,
			       EXISTS (SELECT 1 FROM claimius.claim c WHERE c.id = ? AND c.app_id = a.id) AS claim
			FROM claimius.samna_app a WHERE a.id = ?
		`, app.Org, first, app.ID).Scan(ctx, &mirrored); err != nil {
			t.Fatalf("probe mirror: %v", err)
		}
	}
	probe()
	if mirrored.Home != home || !mirrored.Linked || !mirrored.Claim {
		t.Fatalf("the snapshot must mirror the app with the peer as its home, its organization and its claim, got %+v", mirrored)
	}

	second := postPeerClaim(t, psdb.Instance(), app.ID, app.Org, grantUser, "Mirrored second")
	if n, err := replica.PullSync(ctx); err != nil || n == 0 {
		t.Fatalf("the feed must bring the peer's next write, got %d %v", n, err)
	}
	var held bool
	if err := mirror.NewRaw(`SELECT EXISTS (SELECT 1 FROM claimius.claim WHERE id = ? AND app_id = ?)`, second, app.ID).Scan(ctx, &held); err != nil || !held {
		t.Fatalf("the peer's claim written after the snapshot must reach the mirror, got %v %v", held, err)
	}

	var system, localOrg uuid.UUID
	if err := mirror.NewRaw(`SELECT id, sa_owner_id FROM claimius.get_app()`).Scan(ctx, &system, &localOrg); err != nil {
		t.Fatalf("mirror system app: %v", err)
	}
	local := postPeerClaim(t, mirror, system, localOrg, actor, "Local to the mirror")
	if _, err := psdb.Instance().ExecContext(ctx, `
		INSERT INTO claimius.claim (id, app_id, name, sa_access, sa_owner_id, sa_root_id, sa_created_by)
		VALUES (?, ?, 'Colliding', 4, ?, ?, ?)
	`, local, app.ID, app.Org, app.Org, grantUser); err != nil {
		t.Fatalf("colliding claim on the peer: %v", err)
	}
	if _, err := replica.PullSync(ctx); err != nil {
		t.Fatalf("a colliding row must not fail the pull: %v", err)
	}
	var owner uuid.UUID
	var recorded bool
	if err := mirror.NewRaw(`
		SELECT c.app_id, EXISTS (SELECT 1 FROM claimius.peer_collision pc WHERE pc.object_id = c.id AND pc.prophet = ?)
		FROM claimius.claim c WHERE c.id = ?
	`, home, local).Scan(ctx, &owner, &recorded); err != nil {
		t.Fatalf("probe collision: %v", err)
	}
	if owner != system || !recorded {
		t.Fatalf("a mirrored row whose id the mirror holds for its own app must be refused and recorded, owner %s recorded %v", owner, recorded)
	}

	var cursor struct {
		Txid int64 `bun:"last_applied_txid"`
		Seq  int64 `bun:"last_applied_seq"`
	}
	if err := psdb.Instance().NewRaw(`SELECT last_applied_txid::TEXT::BIGINT AS last_applied_txid, last_applied_seq FROM claimius.sync_consumer WHERE app_id = ? AND consumer_id = ?`,
		app.ID, peer.ID).Scan(ctx, &cursor); err != nil {
		t.Fatalf("the peer must keep the mirror's cursor as a consumer of the app: %v", err)
	}
}
