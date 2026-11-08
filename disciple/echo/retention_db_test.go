//go:build dbtest

package echo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	decho "github.com/nimling/samna-auth-middleware/v2/disciple/echo"
	sdb "github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

type deliveredApp struct {
	Data  shared.AppSeed `json:"data"`
	Count int            `json:"count"`
}

func getRetentionStatus(handler echo.HandlerFunc, method string, userID uuid.UUID, kind, body string) int {
	e := echo.New()
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("kind")
	ctx.SetParamValues(kind)
	ctx.Set("user_id", userID)
	ctx.Set("request_id", uuid.New())
	_ = handler(ctx)
	return rec.Code
}

func TestRetentionIsReadAndEditedThroughAccessOnTheAppsOwnerOrganization(t *testing.T) {
	owner, reader, writer, outsider := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sam/app" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(deliveredApp{Data: shared.AppSeed{PrivateSeed: "00", OwnerID: owner}, Count: 1})
	}))
	t.Cleanup(server.Close)
	t.Setenv("SAUTH_ENDPOINT", server.URL)
	t.Setenv("SAUTH_APP_SLUG", "gotest_retention")
	t.Setenv("SAUTH_APP_SECRET", "gotest-retention-secret")
	t.Setenv("SAUTH_DEBUG_USER_ID", "")
	t.Setenv("SAUTH_DEBUG_CLIENTS", "")
	t.Setenv("SAUTH_DEBUG_PRIVATE_SEED", "")
	client.Bind(server.URL, "")
	t.Cleanup(func() { client.Bind("", "") })
	shared.DeleteAppSeed()
	t.Cleanup(shared.DeleteAppSeed)
	db, err := sdb.Init(echo.New().Logger)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
		DO $$
		DECLARE
			v_owner  UUID := '`+owner.String()+`';
			v_claim  UUID;
		BEGIN
			PERFORM claimius.upsert_app_user(p_user_id => '`+reader.String()+`', p_first_name => 'Reader');
			PERFORM claimius.upsert_app_user(p_user_id => '`+writer.String()+`', p_first_name => 'Writer');
			PERFORM claimius.upsert_app_user(p_user_id => '`+outsider.String()+`', p_first_name => 'Outsider');
			INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
			VALUES (v_owner, 'Retention owner', 'company', v_owner, v_owner, 0, '`+writer.String()+`');
			v_claim := (claimius.insert_claim(p_name => 'Retention reader', p_description => NULL, p_sa_access => 4,
				p_sa_owner_id => v_owner, p_sa_root_id => v_owner, p_sa_created_by => '`+writer.String()+`') -> 'claim' ->> 'id')::UUID;
			PERFORM claimius.insert_claim_object(p_claim_id => v_claim, p_sa_owner_id => v_owner, p_sa_root_id => v_owner,
				p_sa_created_by => '`+writer.String()+`', p_object_id => v_owner, p_object_type => 'claimius.organization', p_sa_access => 4);
			PERFORM claimius.insert_user_claim(p_claim_id => v_claim, p_user_id => '`+reader.String()+`', p_sa_owner_id => v_owner, p_sa_created_by => '`+writer.String()+`');
			v_claim := (claimius.insert_claim(p_name => 'Retention writer', p_description => NULL, p_sa_access => 6,
				p_sa_owner_id => v_owner, p_sa_root_id => v_owner, p_sa_created_by => '`+writer.String()+`') -> 'claim' ->> 'id')::UUID;
			PERFORM claimius.insert_claim_object(p_claim_id => v_claim, p_sa_owner_id => v_owner, p_sa_root_id => v_owner,
				p_sa_created_by => '`+writer.String()+`', p_object_id => v_owner, p_object_type => 'claimius.organization', p_sa_access => 6);
			PERFORM claimius.insert_user_claim(p_claim_id => v_claim, p_user_id => '`+writer.String()+`', p_sa_owner_id => v_owner, p_sa_created_by => '`+writer.String()+`');
		END $$;
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.ExecContext(context.Background(), `UPDATE claimius.retention SET days = 90 WHERE kind = 'notification'`); err != nil {
			t.Errorf("restore notification days: %v", err)
		}
	})

	if code := getRetentionStatus(decho.GetRetentions, http.MethodGet, outsider, "", ""); code != http.StatusForbidden {
		t.Fatalf("a user with no read on the owner organization reads retention: %d", code)
	}
	if code := getRetentionStatus(decho.GetRetentions, http.MethodGet, reader, "", ""); code != http.StatusOK {
		t.Fatalf("a reader of the owner organization reads retention: %d", code)
	}
	if code := getRetentionStatus(decho.PutRetention, http.MethodPut, reader, "notification", `{"days": 3}`); code != http.StatusForbidden {
		t.Fatalf("a reader without write on the owner organization edits retention: %d", code)
	}
	if code := getRetentionStatus(decho.PutRetention, http.MethodPut, writer, "notification", `{"days": 3}`); code != http.StatusOK {
		t.Fatalf("a writer of the owner organization edits retention: %d", code)
	}
}
