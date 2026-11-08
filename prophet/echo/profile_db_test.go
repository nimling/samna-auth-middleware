//go:build dbtest

package echo_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/disciple/sam"
	psam "github.com/nimling/samna-auth-middleware/v2/prophet/sam"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/smw"
)

func readProfile(t *testing.T, raw json.RawMessage) map[string]map[string]shared.ProfileItem {
	t.Helper()
	var profile map[string]map[string]shared.ProfileItem
	if err := json.Unmarshal(raw, &profile); err != nil {
		t.Fatalf("profile %s: %v", raw, err)
	}
	return profile
}

func TestProfileItemsAreWrittenByTheDiscipleIntoTheUsersProfile(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	prophet := psdb.Instance()
	app := postPeerApp(t, grantUser)
	user, stranger := uuid.New(), uuid.New()
	if _, err := prophet.ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Profile user')`, user, app.ID); err != nil {
		t.Fatalf("user: %v", err)
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
	t.Cleanup(func() { disciple.RouteToProphet = false })

	reader := &shared.AccessClaim{UserID: user, Access: shared.ACCESS_READ}
	link := "https://example.no/certificate"
	item := shared.BodySamProfileItem{UserID: user, Field: "certificate", Label: "Certificate", Value: "Solutions Architect", Link: &link}

	first, err := disciple.PutProfileItem(ctx, reader, item)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if got := readProfile(t, first)[app.Slug]["certificate"]; got.Value != "Solutions Architect" || got.Link == nil || *got.Link != link {
		t.Fatalf("the prophet stores the item under the app's slug and the field, got %s", first)
	}

	item.Value = "Solutions Architect Professional"
	second, err := disciple.PutProfileItem(ctx, reader, item)
	if err != nil {
		t.Fatalf("put again: %v", err)
	}
	if got := readProfile(t, second)[app.Slug]; len(got) != 1 || got["certificate"].Value != "Solutions Architect Professional" {
		t.Fatalf("a second put of the same field replaces the item, got %s", second)
	}

	var stored string
	if err := prophet.NewRaw(`SELECT profile::text FROM claimius.samna_user WHERE user_id = ? AND app_id = ?`, user, app.ID).Scan(ctx, &stored); err != nil {
		t.Fatalf("read the user row: %v", err)
	}
	if readProfile(t, json.RawMessage(stored))[app.Slug]["certificate"].Label != "Certificate" {
		t.Fatalf("the item sits on the user's row, got %s", stored)
	}

	bad := item
	bad.Field = "Name!"
	if _, err := disciple.PutProfileItem(ctx, reader, bad); !errors.Is(err, shared.ErrBadProfileItem) {
		t.Fatalf("a malformed field is refused, got %v", err)
	}
	insecure := item
	plain := "http://example.no"
	insecure.Link = &plain
	if _, err := disciple.PutProfileItem(ctx, reader, insecure); !errors.Is(err, shared.ErrBadProfileItem) {
		t.Fatalf("a link that is not https is refused, got %v", err)
	}
	if _, err := disciple.PutProfileItem(ctx, &shared.AccessClaim{UserID: stranger, Access: shared.ACCESS_READ}, item); !errors.Is(err, shared.ErrNoAccess) {
		t.Fatalf("only the user the item is for may write it, got %v", err)
	}
	outsider := item
	outsider.UserID = stranger
	if _, err := disciple.PutProfileItem(ctx, &shared.AccessClaim{UserID: stranger, Access: shared.ACCESS_READ}, outsider); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("a user with no row in the app gets no item, got %v", err)
	}

	field := "certificate"
	after, err := disciple.DeleteProfileItem(ctx, reader, shared.BodySamProfileItemKey{UserID: user, Field: &field})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, present := readProfile(t, after)[app.Slug]; present {
		t.Fatalf("the app's object goes with its last field, got %s", after)
	}
}
