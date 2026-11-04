//go:build dbtest

package echo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	psam "github.com/nimling/samna-auth-middleware/v2/prophet/sam"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/smw"
)

type samAppEnvelope struct {
	Data client.AppSeed `json:"data"`
}

func getSamAppServer(t *testing.T) *httptest.Server {
	t.Helper()
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
	return server
}

func getSamApp(t *testing.T, server *httptest.Server, slug, secret string) (int, string, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/sam/app", nil)
	req.Header.Set("SAuth-App-Slug", slug)
	req.Header.Set("SAuth-App-Secret", secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get /sam/app: %v", err)
	}
	defer resp.Body.Close()
	var body samAppEnvelope
	if resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode /sam/app: %v", err)
		}
	}
	return resp.StatusCode, body.Data.PrivateSeed, resp.Header.Get(echo.HeaderCacheControl)
}

func TestSamAppDeliversTheSeedOnlyToItsOwnApp(t *testing.T) {
	getGrantFixture(t)
	first := postPeerApp(t, grantUser)
	second := postPeerApp(t, grantUser)
	server := getSamAppServer(t)

	status, seed, cache := getSamApp(t, server, first.Slug, first.Secret)
	if status != http.StatusOK || seed != first.Seed {
		t.Fatalf("the app must receive its own opened seed, got %d %q", status, seed)
	}
	if cache != "no-store" {
		t.Fatalf("the seed must not be cached on the way, got Cache-Control %q", cache)
	}
	status, seed, _ = getSamApp(t, server, second.Slug, second.Secret)
	if status != http.StatusOK || seed != second.Seed || seed == first.Seed {
		t.Fatalf("each app must receive only its own seed, got %d %q", status, seed)
	}
	if status, seed, _ := getSamApp(t, server, first.Slug, second.Secret); status != http.StatusUnauthorized || seed != "" {
		t.Fatalf("another app's secret must not open the seed, got %d %q", status, seed)
	}
	if status, _, _ := getSamApp(t, server, first.Slug, ""); status != http.StatusUnauthorized {
		t.Fatalf("an unauthenticated read must be refused, got %d", status)
	}
}

func TestSamAppRefusesWithoutAMasterKey(t *testing.T) {
	getGrantFixture(t)
	app := postPeerApp(t, grantUser)
	server := getSamAppServer(t)
	t.Setenv("SAUTH_MASTER_KEY", "")
	if status, seed, _ := getSamApp(t, server, app.Slug, app.Secret); status == http.StatusOK || seed != "" {
		t.Fatalf("a prophet without its master key must not answer a seed, got %d %q", status, seed)
	}
}

func TestSamInitiateAppsListsOnlyTheInitiateAppsOfTheCaller(t *testing.T) {
	getGrantFixture(t)
	root := postPeerApp(t, grantUser)
	other := postPeerApp(t, grantUser)
	initiate := "gotest_initiate_" + uuid.NewString()[:8]
	if _, err := psdb.Instance().ExecContext(context.Background(), `
		INSERT INTO claimius.samna_app (slug, name, redirect_uris, private_key, private_seed, sa_owner_id, sa_created_by, sa_parent_id)
		VALUES (?, 'Initiate app', ARRAY['https://web.example'], 'key', 'seed', ?, ?, ?)
	`, initiate, root.Org, grantUser, root.ID); err != nil {
		t.Fatalf("initiate app: %v", err)
	}
	server := getSamAppServer(t)

	get := func(slug, secret string) []shared.SamnaApp {
		req, _ := http.NewRequest(http.MethodGet, server.URL+"/sam/app/initiate", nil)
		req.Header.Set("SAuth-App-Slug", slug)
		req.Header.Set("SAuth-App-Secret", secret)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("get /sam/app/initiate: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get /sam/app/initiate: status %d", resp.StatusCode)
		}
		var body struct {
			Data []shared.SamnaApp `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode /sam/app/initiate: %v", err)
		}
		return body.Data
	}

	apps := get(root.Slug, root.Secret)
	if len(apps) != 1 || apps[0].Slug != initiate || apps[0].GetPrimaryOrigin() != "https://web.example" || apps[0].RootID != root.ID {
		t.Fatalf("the root must list its one initiate app with its origin, got %+v", apps)
	}
	if apps := get(other.Slug, other.Secret); len(apps) != 0 {
		t.Fatalf("another root must list none, got %+v", apps)
	}
}
