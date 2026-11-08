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

func readProfile(t *testing.T, raw json.RawMessage) map[string]shared.ProfileItem {
	t.Helper()
	var profile map[string]shared.ProfileItem
	if err := json.Unmarshal(raw, &profile); err != nil {
		t.Fatalf("profile %s: %v", raw, err)
	}
	return profile
}

func TestProfileEntriesCarryASchemaAndAValue(t *testing.T) {
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
	schema := shared.ProfileSchema{Type: "string", Title: "Certificate", Format: "uri"}
	item := shared.BodySamProfileItem{UserID: user, Field: "certificate", Schema: schema, Value: json.RawMessage(`"https://example.no/certificate"`)}

	first, err := disciple.PutProfileItem(ctx, reader, item)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	key := app.Slug + ".certificate"
	if got := readProfile(t, first)[key]; string(got.Value) != `"https://example.no/certificate"` || got.Schema.Type != "string" {
		t.Fatalf("the prophet stores the entry under the app's slug and the field with its schema and value, got %s", first)
	}

	item.Value = json.RawMessage(`"https://example.no/certificate/2"`)
	second, err := disciple.PutProfileItem(ctx, reader, item)
	if err != nil {
		t.Fatalf("put again: %v", err)
	}
	if got := readProfile(t, second)[key]; string(got.Value) != `"https://example.no/certificate/2"` {
		t.Fatalf("a second put of the same field replaces the entry, got %s", second)
	}

	own, err := psdb.PutProfile(ctx, user, map[string]*shared.ProfileItem{
		"bio": {Schema: shared.ProfileSchema{Type: "string", Format: "multiline"}, Value: json.RawMessage(`"Builds access control"`)},
	})
	if err != nil {
		t.Fatalf("own keys: %v", err)
	}
	if got := readProfile(t, own); string(got["bio"].Value) != `"Builds access control"` || string(got[key].Value) != `"https://example.no/certificate/2"` {
		t.Fatalf("the user's own keys sit beside the app's entries, got %s", own)
	}
	if _, err := psdb.PutProfile(ctx, user, map[string]*shared.ProfileItem{key: {Schema: schema, Value: json.RawMessage(`"x"`)}}); !errors.Is(err, shared.ErrBadProfileItem) {
		t.Fatalf("the user cannot write an app's key, got %v", err)
	}
	if _, err := psdb.PutProfile(ctx, user, map[string]*shared.ProfileItem{"bio": {Schema: shared.ProfileSchema{Title: "no type"}, Value: json.RawMessage(`"x"`)}}); !errors.Is(err, shared.ErrBadProfileItem) {
		t.Fatalf("a schema without a type is refused, got %v", err)
	}
	cleared, err := psdb.PutProfile(ctx, user, map[string]*shared.ProfileItem{"bio": nil})
	if err != nil {
		t.Fatalf("remove own key: %v", err)
	}
	if _, present := readProfile(t, cleared)["bio"]; present {
		t.Fatalf("a null entry removes the key, got %s", cleared)
	}

	bad := item
	bad.Field = "Name!"
	if _, err := disciple.PutProfileItem(ctx, reader, bad); !errors.Is(err, shared.ErrBadProfileItem) {
		t.Fatalf("a malformed field is refused, got %v", err)
	}
	noSchema := item
	noSchema.Schema = shared.ProfileSchema{}
	if _, err := disciple.PutProfileItem(ctx, reader, noSchema); !errors.Is(err, shared.ErrBadProfileItem) {
		t.Fatalf("a schema that is not an object is refused, got %v", err)
	}
	if _, err := disciple.PutProfileItem(ctx, &shared.AccessClaim{UserID: stranger, Access: shared.ACCESS_READ}, item); !errors.Is(err, shared.ErrNoAccess) {
		t.Fatalf("only the user the entry is for may write it, got %v", err)
	}
	outsider := item
	outsider.UserID = stranger
	if _, err := disciple.PutProfileItem(ctx, &shared.AccessClaim{UserID: stranger, Access: shared.ACCESS_READ}, outsider); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("a user with no row in the app gets no entry, got %v", err)
	}

	field := "certificate"
	after, err := disciple.DeleteProfileItem(ctx, reader, shared.BodySamProfileItemKey{UserID: user, Field: &field})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, present := readProfile(t, after)[key]; present {
		t.Fatalf("the app's entry is gone, got %s", after)
	}
}
