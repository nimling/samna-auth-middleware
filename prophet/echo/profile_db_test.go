//go:build dbtest

package echo_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
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

func TestUserNameIsReservedOncePerApp(t *testing.T) {
	getGrantFixture(t)
	ctx := context.Background()
	prophet := psdb.Instance()
	app := postPeerApp(t, grantUser)
	other := postPeerApp(t, grantUser)
	owner, rival := uuid.New(), uuid.New()
	for _, user := range []uuid.UUID{owner, rival} {
		for _, appID := range []uuid.UUID{app.ID, other.ID} {
			if _, err := prophet.ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Name user')`, user, appID); err != nil {
				t.Fatalf("user: %v", err)
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
	disciple.RouteToProphet = true
	t.Cleanup(func() { disciple.RouteToProphet = false })

	if got := shared.GetProfileLink(owner); got != server.URL+"/u/"+owner.String() {
		t.Fatalf("the profile link is the samna_auth address with the user id, got %s", got)
	}

	name := "name-" + strings.ReplaceAll(owner.String()[:8], "-", "")
	ownerClaim := &shared.AccessClaim{UserID: owner, Access: shared.ACCESS_READ}
	rivalClaim := &shared.AccessClaim{UserID: rival, Access: shared.ACCESS_READ}

	free, err := disciple.GetUserNameAvailable(ctx, ownerClaim, name)
	if err != nil {
		t.Fatalf("available: %v", err)
	}
	if !free.Available || free.UserName != name {
		t.Fatalf("a name nobody holds is available, got %+v", free)
	}
	reserved, err := disciple.PutUserName(ctx, ownerClaim, shared.BodySamUserName{UserID: owner, UserName: &name})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if reserved == nil || *reserved != name {
		t.Fatalf("the reserve returns the name, got %v", reserved)
	}
	if again, err := disciple.PutUserName(ctx, ownerClaim, shared.BodySamUserName{UserID: owner, UserName: &name}); err != nil || *again != name {
		t.Fatalf("the holder reserves its own name again, got %v %v", again, err)
	}
	taken, err := disciple.GetUserNameAvailable(ctx, rivalClaim, name)
	if err != nil {
		t.Fatalf("available after reserve: %v", err)
	}
	if taken.Available {
		t.Fatalf("a reserved name is not available, got %+v", taken)
	}
	if _, err := disciple.PutUserName(ctx, rivalClaim, shared.BodySamUserName{UserID: rival, UserName: &name}); !errors.Is(err, shared.ErrUserNameTaken) {
		t.Fatalf("a taken name is refused, got %v", err)
	}
	if _, err := prophet.ExecContext(ctx, `UPDATE claimius.samna_user SET user_name = upper(?) WHERE user_id = ? AND app_id = ?`, name, rival, app.ID); err == nil {
		t.Fatalf("the index refuses the same name in another case in one app")
	}
	if _, err := psdb.PutUserName(ctx, other.ID, shared.BodySamUserName{UserID: rival, UserName: &name}); err != nil {
		t.Fatalf("the same name is free in another app, got %v", err)
	}

	if _, err := disciple.PutUserName(ctx, rivalClaim, shared.BodySamUserName{UserID: owner, UserName: &name}); !errors.Is(err, shared.ErrNoAccess) {
		t.Fatalf("only the user reserves a name for itself, got %v", err)
	}
	for _, bad := range []string{"me", "Upper", "1digit", uuid.NewString(), "with space"} {
		if _, err := disciple.GetUserNameAvailable(ctx, ownerClaim, bad); !errors.Is(err, shared.ErrBadUserName) {
			t.Errorf("%q is not a user name, got %v", bad, err)
		}
		if _, err := psdb.GetUserNameAvailable(ctx, app.ID, bad); !errors.Is(err, shared.ErrBadUserName) {
			t.Errorf("the prophet refuses %q too, got %v", bad, err)
		}
	}

	released, err := disciple.PutUserName(ctx, ownerClaim, shared.BodySamUserName{UserID: owner})
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if released != nil {
		t.Fatalf("a null name releases it, got %v", *released)
	}
	if _, err := disciple.PutUserName(ctx, rivalClaim, shared.BodySamUserName{UserID: rival, UserName: &name}); err != nil {
		t.Fatalf("a released name reserves for the next user, got %v", err)
	}

	carried := uuid.New()
	if _, err := prophet.ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_user_name => ?)`, carried, app.ID, name); err != nil {
		t.Fatalf("a login carrying a taken name still gets its row, got %v", err)
	}
	var carriedName *string
	if err := prophet.NewRaw(`SELECT user_name FROM claimius.samna_user WHERE user_id = ? AND app_id = ?`, carried, app.ID).Scan(ctx, &carriedName); err != nil {
		t.Fatalf("carried row: %v", err)
	}
	if carriedName != nil {
		t.Fatalf("a taken name does not land on a new row, got %s", *carriedName)
	}

	if _, err := prophet.ExecContext(ctx, `SELECT claimius.merge_user(?, ?)`, owner, rival); err != nil {
		t.Fatalf("merging the holder of a name into another user of the app, got %v", err)
	}
	var merged *string
	if err := prophet.NewRaw(`SELECT user_name FROM claimius.samna_user WHERE user_id = ? AND app_id = ? AND sa_deleted_at IS NULL`, owner, app.ID).Scan(ctx, &merged); err != nil {
		t.Fatalf("merged row: %v", err)
	}
	if merged == nil || *merged != name {
		t.Fatalf("the merge carries the source name to the target, got %v", merged)
	}
}
