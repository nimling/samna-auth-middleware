//go:build dbtest

package echo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	pecho "github.com/nimling/samna-auth-middleware/v2/prophet/echo"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
)

type guestReply struct {
	Data struct {
		UserID uuid.UUID `json:"user_id"`
	} `json:"data"`
}

func postGuest(t *testing.T, appID, claimID, createdBy uuid.UUID, user map[string]any, endsAt time.Time) (int, uuid.UUID) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"user": user, "claim_id": claimID, "owner_id": grantRoot, "ends_at": endsAt, "created_by": createdBy})
	if err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(body)))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.Set("app_id", appID)
	ctx.Set("request_id", uuid.New())
	_ = pecho.PostUserGuest(ctx)
	var reply guestReply
	_ = json.Unmarshal(rec.Body.Bytes(), &reply)
	return rec.Code, reply.Data.UserID
}

func getGuestState(t *testing.T, userID uuid.UUID) (string, bool, time.Time) {
	t.Helper()
	var kind string
	var deleted bool
	var endsAt time.Time
	if err := psdb.Instance().NewRaw(
		`SELECT u.type::text, u.sa_deleted_at IS NOT NULL, g.ends_at FROM claimius.samna_user u JOIN claimius.guest_identity g ON g.user_id = u.user_id
		  WHERE u.user_id = ? ORDER BY u.sa_deleted_at IS NOT NULL LIMIT 1`, userID,
	).Scan(context.Background(), &kind, &deleted, &endsAt); err != nil {
		t.Fatalf("guest %s: %v", userID, err)
	}
	return kind, deleted, endsAt
}

func TestPostUserGuestUpsertsReusesAndExpires(t *testing.T) {
	appID, claimID := getGrantFixture(t)
	ctx := context.Background()
	var caller uuid.UUID
	if err := psdb.Instance().NewRaw(`SELECT id FROM claimius.user_claim WHERE claim_id = ? AND user_id = ? AND sa_deleted_at IS NULL`, claimID, grantUser).Scan(ctx, &caller); err != nil {
		t.Fatalf("caller grant: %v", err)
	}
	email := "guest." + uuid.NewString() + "@example.no"
	soon := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	later := soon.Add(48 * time.Hour)

	status, first := postGuest(t, appID, claimID, caller, map[string]any{"first_name": "Gjest", "email": email}, soon)
	if status != http.StatusCreated || first == uuid.Nil {
		t.Fatalf("first registration: want 201 with a user, got %d %s", status, first)
	}
	if kind, deleted, _ := getGuestState(t, first); kind != "guest" || deleted {
		t.Fatalf("first registration: want a live guest, got %s deleted=%v", kind, deleted)
	}

	status, again := postGuest(t, appID, claimID, caller, map[string]any{"email": strings.ToUpper(email)}, later)
	if status != http.StatusOK || again != first {
		t.Fatalf("same email: want 200 and the same guest %s, got %d %s", first, status, again)
	}
	if _, _, endsAt := getGuestState(t, first); !endsAt.Equal(later) {
		t.Fatalf("same email: want ends_at moved to %s, got %s", later, endsAt)
	}

	status, earlier := postGuest(t, appID, claimID, caller, map[string]any{"email": email}, soon)
	if status != http.StatusOK || earlier != first {
		t.Fatalf("earlier ends_at: want 200 and the same guest, got %d %s", status, earlier)
	}
	if _, _, endsAt := getGuestState(t, first); !endsAt.Equal(later) {
		t.Fatalf("earlier ends_at: want ends_at kept at %s, got %s", later, endsAt)
	}

	status, anonymous := postGuest(t, appID, claimID, caller, map[string]any{"first_name": "Uten kontakt"}, soon)
	if status != http.StatusCreated || anonymous == first {
		t.Fatalf("no email or phone: want 201 and a new guest, got %d %s", status, anonymous)
	}

	regular := uuid.New()
	shared := "shared." + uuid.NewString() + "@example.no"
	if _, err := psdb.Instance().ExecContext(ctx, `INSERT INTO claimius.samna_user (user_id, first_name, email, type) VALUES (?, 'Vanlig', ?, 'user')`, regular, shared); err != nil {
		t.Fatalf("regular user: %v", err)
	}
	status, beside := postGuest(t, appID, claimID, caller, map[string]any{"email": shared}, soon)
	if status != http.StatusCreated || beside == regular {
		t.Fatalf("email of a regular user: want 201 and a guest beside it, got %d %s", status, beside)
	}

	if _, err := psdb.Instance().ExecContext(ctx, `UPDATE claimius.guest_identity SET ends_at = now() - interval '1 hour' WHERE user_id = ?`, first); err != nil {
		t.Fatalf("expire: %v", err)
	}
	if _, err := psdb.Instance().ExecContext(ctx, `SELECT claimius.prune('{}'::jsonb)`); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, deleted, _ := getGuestState(t, first); !deleted {
		t.Fatal("expired: want the guest soft deleted by prune")
	}

	status, back := postGuest(t, appID, claimID, caller, map[string]any{"email": email}, later)
	if status != http.StatusOK || back != first {
		t.Fatalf("after expiry: want 200 and the same guest undeleted, got %d %s", status, back)
	}
	if kind, deleted, endsAt := getGuestState(t, first); kind != "guest" || deleted || !endsAt.Equal(later) {
		t.Fatalf("after expiry: want a live guest ending %s, got %s deleted=%v %s", later, kind, deleted, endsAt)
	}

	if status, _ := postGuest(t, appID, claimID, caller, map[string]any{"email": email}, time.Time{}); status != http.StatusBadRequest {
		t.Fatalf("no ends_at: want 400, got %d", status)
	}
}
