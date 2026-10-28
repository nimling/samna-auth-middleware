//go:build dbtest

package echo_test

import (
	"context"
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

func TestProfileItemsAreFlaggedByTheDiscipleAndStoredOnTheProphet(t *testing.T) {
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
	certificate := uuid.New()
	link := "https://example.no/certificate"
	flag := shared.BodySamProfileItem{
		UserID: user, ObjectType: "public.certificate", ObjectID: certificate,
		Field: "name", Label: "Certificate", Value: "Solutions Architect", Link: &link,
	}

	first, err := disciple.PutProfileItem(ctx, reader, flag)
	if err != nil {
		t.Fatalf("flag: %v", err)
	}
	if first.AppID != app.ID || first.AppSlug != app.Slug || first.Value != "Solutions Architect" || first.Link == nil || *first.Link != link {
		t.Fatalf("the prophet stores the flag under the app it came from, got %+v", first)
	}

	flag.Value = "Solutions Architect Professional"
	second, err := disciple.PutProfileItem(ctx, reader, flag)
	if err != nil {
		t.Fatalf("reflag: %v", err)
	}
	if second.ID != first.ID || second.Value != "Solutions Architect Professional" {
		t.Fatalf("a second flag of the same object and field replaces the first, got %+v after %+v", second, first)
	}

	listed, err := psdb.GetProfileItems(ctx, user, nil)
	if err != nil || len(listed) != 1 || listed[0].ID != first.ID || listed[0].AppName == "" {
		t.Fatalf("the prophet lists the user's flags with the app name, got %+v %v", listed, err)
	}
	mine, err := disciple.GetProfileItems(ctx, user)
	if err != nil || len(mine) != 1 || mine[0].ID != first.ID {
		t.Fatalf("the disciple reads back its own flags, got %+v %v", mine, err)
	}

	bad := flag
	bad.Field = "Name!"
	if _, err := disciple.PutProfileItem(ctx, reader, bad); !errors.Is(err, shared.ErrBadProfileItem) {
		t.Fatalf("a malformed field is refused, got %v", err)
	}
	insecure := flag
	plain := "http://example.no"
	insecure.Link = &plain
	if _, err := disciple.PutProfileItem(ctx, reader, insecure); !errors.Is(err, shared.ErrBadProfileItem) {
		t.Fatalf("a link that is not https is refused, got %v", err)
	}
	if _, err := disciple.PutProfileItem(ctx, &shared.AccessClaim{UserID: stranger, Access: shared.ACCESS_READ}, flag); !errors.Is(err, shared.ErrNoAccess) {
		t.Fatalf("only the user the flag is for may flag it, got %v", err)
	}
	outsider := flag
	outsider.UserID = stranger
	if _, err := disciple.PutProfileItem(ctx, &shared.AccessClaim{UserID: stranger, Access: shared.ACCESS_READ}, outsider); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("a user with no row in the app cannot be flagged, got %v", err)
	}

	field := "name"
	withdrawn, err := disciple.DeleteProfileItem(ctx, reader, shared.BodySamProfileItemKey{UserID: user, ObjectType: "public.certificate", ObjectID: certificate, Field: &field})
	if err != nil || len(withdrawn) != 1 || withdrawn[0].ID != first.ID {
		t.Fatalf("withdraw: %+v %v", withdrawn, err)
	}
	if after, err := psdb.GetProfileItems(ctx, user, nil); err != nil || len(after) != 0 {
		t.Fatalf("a withdrawn flag is gone from the profile, got %+v %v", after, err)
	}
}
