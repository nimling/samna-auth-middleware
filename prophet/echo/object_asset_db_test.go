//go:build dbtest

package echo_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	"github.com/nimling/samna-auth-middleware/v2/client"
	"github.com/nimling/samna-auth-middleware/v2/disciple/sam"
	psam "github.com/nimling/samna-auth-middleware/v2/prophet/sam"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/smw"
)

func TestADiscipleLinksAnAssetThroughTheProphetAndReadsItBack(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	prophet := psdb.Instance()
	app := postPeerApp(t, grantUser)

	var authorized []string
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorized = append(authorized, r.URL.Path+" "+strings.SplitN(r.Header.Get("Authorization"), " ", 2)[0])
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("ETag", `"home"`)
		w.Write([]byte("logo-bytes"))
	}))
	defer home.Close()
	if _, err := prophet.ExecContext(ctx, `UPDATE claimius.samna_app SET redirect_uris = ? WHERE id = ?`,
		pq.Array([]string{home.URL + "/api/auth/callback"}), app.ID); err != nil {
		t.Fatalf("home callback: %v", err)
	}

	var owners uuid.UUID
	if err := prophet.NewRaw(`
		SELECT (claimius.insert_claim(p_name => 'Asset owners', p_description => NULL, p_sa_access => 15,
		        p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?) -> 'claim' ->> 'id')::UUID
	`, app.Org, app.Org, grantUser).Scan(ctx, &owners); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := prophet.ExecContext(ctx, `
		SELECT claimius.insert_claim_object(p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?,
		       p_sa_created_by => ?, p_object_id => ?, p_object_type => 'claimius.organization', p_inherits => TRUE)
	`, owners, app.Org, app.Org, grantUser, app.Org); err != nil {
		t.Fatalf("bind: %v", err)
	}
	owner, outsider := uuid.New(), uuid.New()
	var ownerGrant uuid.UUID
	if err := prophet.NewRaw(`
		SELECT (claimius.insert_user_claim(p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?)
		        -> 'user_claim' ->> 'id')::UUID
		FROM claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Owner')
	`, owners, owner, app.Org, grantUser, owner, app.ID).Scan(ctx, &ownerGrant); err != nil {
		t.Fatalf("grant: %v", err)
	}
	grantless := uuid.New()
	if _, err := prophet.ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Grantless')`, grantless, app.ID); err != nil {
		t.Fatalf("grantless: %v", err)
	}
	var members uuid.UUID
	if err := prophet.NewRaw(`
		SELECT (claimius.insert_claim(p_name => 'Asset members', p_description => NULL, p_sa_access => 4,
		        p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?) -> 'claim' ->> 'id')::UUID
	`, app.Org, app.Org, grantUser).Scan(ctx, &members); err != nil {
		t.Fatalf("default claim: %v", err)
	}
	if _, err := prophet.ExecContext(ctx, `UPDATE claimius.samna_app SET claim_id = ? WHERE id = ?`, members, app.ID); err != nil {
		t.Fatalf("set default claim: %v", err)
	}
	var outsiderGrant uuid.UUID
	if err := prophet.NewRaw(`SELECT (claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Outsider') -> 'user_claim' ->> 'id')::UUID`,
		outsider, app.ID).Scan(ctx, &outsiderGrant); err != nil {
		t.Fatalf("outsider: %v", err)
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
	disciple.RouteToProphet = true
	defer func() { disciple.RouteToProphet = false }()

	ac, err := disciple.MutationAccess(ctx, owner, app.Org, shared.OBJECT_TYPE_ORGANIZATION, shared.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("owner access: %v", err)
	}
	logo := &shared.ObjectAsset{ObjectType: shared.OBJECT_TYPE_ORGANIZATION, ObjectID: app.Org, AssetID: uuid.New(), Kind: "logo", MimeType: "image/png"}
	if err := disciple.NewInsert().Model(logo).Access(ac).Scan(ctx); err != nil {
		t.Fatalf("an owner of the organization links an asset to it through the prophet, got %v", err)
	}
	var stored struct {
		AppID     uuid.UUID  `bun:"app_id"`
		CreatedBy *uuid.UUID `bun:"sa_created_by"`
	}
	if err := prophet.NewRaw(`SELECT app_id, sa_created_by FROM claimius.object_asset WHERE id = ?`, logo.ID).Scan(ctx, &stored); err != nil {
		t.Fatalf("read the stored link: %v", err)
	}
	if stored.AppID != app.ID || stored.CreatedBy == nil || *stored.CreatedBy != ownerGrant {
		t.Fatalf("the prophet stamps the calling app and the actor's grant, got %+v", stored)
	}

	refused := disciple.NewInsert().Model(&shared.ObjectAsset{ObjectType: shared.OBJECT_TYPE_ORGANIZATION, ObjectID: app.Org,
		AssetID: uuid.New(), Kind: "logo", MimeType: "image/png"}).Access(&shared.AccessClaim{UserID: outsider, OwnerID: app.Org}).Scan(ctx)
	if !errors.Is(refused, shared.ErrNoAccess) {
		t.Fatalf("a user without write on the organization is refused by the prophet, got %v", refused)
	}

	self, err := disciple.MutationAccess(ctx, outsider, outsider, shared.OBJECT_TYPE_USER, shared.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("self access: %v", err)
	}
	profile := &shared.ObjectAsset{ObjectType: shared.OBJECT_TYPE_USER, ObjectID: outsider, AssetID: uuid.New(), Kind: "profile", MimeType: "image/png"}
	if err := disciple.NewInsert().Model(profile).Access(self).Scan(ctx); err != nil {
		t.Fatalf("a user links an asset to itself, got %v", err)
	}
	if profile.CreatedBy == nil || *profile.CreatedBy != outsiderGrant || profile.OwnerID != outsider {
		t.Fatalf("a link on the writer's own user carries the writer's default grant and the user as owner, got %+v", profile)
	}
	alone, err := disciple.MutationAccess(ctx, grantless, grantless, shared.OBJECT_TYPE_USER, shared.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("grantless self access: %v", err)
	}
	orphan := disciple.NewInsert().Model(&shared.ObjectAsset{ObjectType: shared.OBJECT_TYPE_USER, ObjectID: grantless,
		AssetID: uuid.New(), Kind: "profile", MimeType: "image/png"}).Access(alone).Scan(ctx)
	if !errors.Is(orphan, shared.ErrNoAccess) {
		t.Fatalf("a user without a grant in the home app is refused a link on itself, got %v", orphan)
	}

	if _, err := replica.PullSync(ctx); err != nil {
		t.Fatalf("pull: %v", err)
	}
	read, err := disciple.GetObjectAssets(ctx, owner, shared.OBJECT_TYPE_ORGANIZATION, app.Org, "logo")
	if err != nil || len(read) != 1 || read[0].ID != logo.ID {
		t.Fatalf("the link reaches the disciple and its reader, got %v %v", read, err)
	}
	if hidden, err := disciple.GetObjectAssets(ctx, outsider, shared.OBJECT_TYPE_ORGANIZATION, app.Org, ""); err != nil || len(hidden) != 0 {
		t.Fatalf("a reader without read on the organization sees none of its links, got %v %v", hidden, err)
	}
	if mine, err := disciple.GetObjectAsset(ctx, outsider, profile.ID); err != nil || mine.ID != profile.ID {
		t.Fatalf("a user reads the link on itself by id, got %v %v", mine, err)
	}

	resp, err := client.GetStream(ctx, "object_asset/"+logo.ID.String()+"/data", nil)
	if err != nil {
		t.Fatalf("fetch through the prophet: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "logo-bytes" || resp.Header.Get("ETag") != `"home"` {
		t.Fatalf("the prophet relays the home's answer, got %d %q %v", resp.StatusCode, body, resp.Header)
	}
	if len(authorized) != 1 || authorized[0] != "/api/auth/asset/"+logo.AssetID.String()+"/data Bearer" {
		t.Fatalf("the prophet asks the home for the asset by its id with a bearer token, got %v", authorized)
	}

	if _, err := disciple.NewDelete().Model(&shared.ObjectAsset{ID: logo.ID}).Access(ac).Exec(ctx); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var live int
	if err := prophet.NewRaw(`SELECT count(*) FROM claimius.object_asset WHERE id = ? AND sa_deleted_at IS NULL`, logo.ID).Scan(ctx, &live); err != nil || live != 0 {
		t.Fatalf("a routed delete soft deletes the link on the prophet, got %d %v", live, err)
	}
	if _, err := client.GetStream(ctx, "object_asset/"+logo.ID.String()+"/data", nil); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("a deleted link serves no file, got %v", err)
	}
}
