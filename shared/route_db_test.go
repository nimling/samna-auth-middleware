//go:build dbtest

package shared_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	sdb "github.com/nimling/samna-auth-middleware/v2/shared"
)

type routedCall struct {
	method    string
	path      string
	createdBy string
}

func stubProphet(t *testing.T, db *sdb.DB) (*[]routedCall, func()) {
	t.Helper()
	var mu sync.Mutex
	calls := make([]routedCall, 0)
	prophet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		row := map[string]any{}
		_ = json.Unmarshal(raw, &row)
		createdBy, _ := row["created_by"].(string)
		mu.Lock()
		calls = append(calls, routedCall{method: r.Method, path: r.URL.Path, createdBy: createdBy})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete {
			w.Write([]byte(`{"data":null,"count":0}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/user/token") {
			var user sdb.SamnaUser
			if err := db.NewSelect().Model(&user).Where("user_id = ?", userB).Where("sa_deleted_at IS NULL").Limit(1).Scan(r.Context()); err != nil {
				t.Errorf("read the user the stub answers with: %v", err)
			}
			body, _ := json.Marshal(map[string]any{"data": user, "count": 1})
			w.WriteHeader(http.StatusCreated)
			w.Write(body)
			return
		}
		if id, ok := row["id"].(string); !ok || id == "" || id == uuid.Nil.String() {
			row["id"] = uuid.New().String()
		}
		body, _ := json.Marshal(map[string]any{"data": row, "count": 1})
		w.Write(body)
	}))
	t.Setenv("SAUTH_ENDPOINT", prophet.URL)
	return &calls, prophet.Close
}

func countRows(t *testing.T, db *sdb.DB, table string) int {
	t.Helper()
	var n int
	if err := db.NewRaw("SELECT count(*) FROM "+table).Scan(context.Background(), &n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestEveryWriteHelperOfADiscipleGoesToTheProphetFirst(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	ac, err := db.MutationAccess(ctx, userA, orgRoot, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	calls, stop := stubProphet(t, db)
	defer stop()
	db.RouteToProphet = true
	tokensBefore := countRows(t, db, "claimius.user_token")

	claim, err := db.CreateClaim(ctx, ac, sdb.CreateClaimParams{Name: "Routed claim", Access: sdb.ACCESS_READ})
	if err != nil {
		t.Fatalf("create claim: %v", err)
	}
	organization := sdb.ObjectType(sdb.OBJECT_TYPE_ORGANIZATION)
	if _, err := claim.AssignObject(&sdb.ClaimObject{ObjectID: &orgAlpha, ObjectType: &organization, Inherits: true, OwnerID: orgRoot, RootID: orgRoot}); err != nil {
		t.Fatalf("assign object: %v", err)
	}
	if _, err := claim.AssignUser(userB); err != nil {
		t.Fatalf("assign user: %v", err)
	}
	if _, err := db.CreateVirtualClaim(ctx, ac, sdb.CreateVirtualClaimParams{
		Name: "Routed share", Access: sdb.ACCESS_READ, OwnerID: orgRoot,
		ObjectID: orgAlpha, ObjectType: string(sdb.OBJECT_TYPE_ORGANIZATION), TargetUserID: userB,
	}); err != nil {
		t.Fatalf("create virtual claim: %v", err)
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "routed-identity"}).SignedString([]byte("test"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	_, created, err := db.IssueUserToken(ctx, ac, sdb.SamnaUserToken{Token: token, OwnerID: &orgRoot, ClaimID: ac.ClaimID})
	if err != nil {
		t.Fatalf("issue user token: %v", err)
	}
	if !created {
		t.Fatal("a routed token write must answer created from the prophet's 201")
	}
	if _, err := db.CreateOrganizationUser(ctx, ac, &sdb.SamnaUser{UserID: uuid.New(), Email: "routed@example.com", OwnerID: &orgRoot, ClaimID: &claim.ID}); err != nil {
		t.Fatalf("create organization user: %v", err)
	}
	if _, err := db.DeleteUser(ctx, ac, userB); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	want := []string{
		"POST /claim", "POST /object", "POST /user",
		"POST /claim", "POST /object", "POST /user",
		"POST /user/token", "POST /user", "DELETE /user/" + userB.String(),
	}
	if len(*calls) != len(want) {
		t.Fatalf("the prophet must receive %d writes, got %v", len(want), *calls)
	}
	for i, c := range *calls {
		parts := strings.SplitN(want[i], " ", 2)
		if c.method != parts[0] || !strings.HasSuffix(c.path, parts[1]) {
			t.Fatalf("write %d must be %s, got %s %s", i, want[i], c.method, c.path)
		}
		if c.method == http.MethodPost && c.createdBy != ac.UserClaimID.String() {
			t.Fatalf("write %d must carry the actor %s, got %q", i, ac.UserClaimID, c.createdBy)
		}
	}
	if after := countRows(t, db, "claimius.user_token"); after != tokensBefore {
		t.Fatalf("a routed token write must not issue a local token, %d before and %d after", tokensBefore, after)
	}
	var stillLive bool
	if err := db.NewRaw(`SELECT EXISTS (SELECT 1 FROM claimius.samna_user WHERE user_id = ? AND sa_deleted_at IS NULL)`, userB).Scan(ctx, &stillLive); err != nil {
		t.Fatalf("read user: %v", err)
	}
	if !stillLive {
		t.Fatal("a routed delete must leave the local user to the sync, not soft delete it locally")
	}
}

func TestADiscipleInDebugModeWritesTheSameHelpersLocally(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	ac, err := db.MutationAccess(ctx, userA, orgRoot, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	calls, stop := stubProphet(t, db)
	defer stop()
	db.RouteToProphet = false

	claim, err := db.CreateClaim(ctx, ac, sdb.CreateClaimParams{Name: "Local claim", Access: sdb.ACCESS_READ})
	if err != nil {
		t.Fatalf("create claim: %v", err)
	}
	if _, err := claim.AssignUser(userB); err != nil {
		t.Fatalf("assign user: %v", err)
	}
	member := uuid.New()
	if _, err := db.CreateOrganizationUser(ctx, ac, &sdb.SamnaUser{UserID: member, Email: "local@example.com", OwnerID: &orgRoot, ClaimID: &claim.ID}); err != nil {
		t.Fatalf("create organization user: %v", err)
	}
	if _, err := db.CreateOrganizationUser(ctx, ac, &sdb.SamnaUser{UserID: member, OwnerID: &orgRoot, ClaimID: &claim.ID}); !errors.Is(err, sdb.ErrUserAlreadyExists) {
		t.Fatalf("a second create of the same user must answer ErrUserAlreadyExists, got %v", err)
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "local-identity"}).SignedString([]byte("test"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	first, created, err := db.IssueUserToken(ctx, ac, sdb.SamnaUserToken{Token: token, OwnerID: &orgRoot, ClaimID: &claim.ID})
	if err != nil || !created {
		t.Fatalf("the first token write must create the user, created %v, err %v", created, err)
	}
	again, created, err := db.IssueUserToken(ctx, ac, sdb.SamnaUserToken{Token: token, OwnerID: &orgRoot, ClaimID: &claim.ID})
	if err != nil || created || again.UserID != first.UserID {
		t.Fatalf("the second token write must find the same user, created %v, err %v", created, err)
	}
	if len(*calls) != 0 {
		t.Fatalf("a local write must not reach the prophet, got %v", *calls)
	}
	var held bool
	if err := db.NewRaw(`SELECT EXISTS (SELECT 1 FROM claimius.user_claim WHERE claim_id = ? AND user_id = ? AND sa_deleted_at IS NULL)`, claim.ID, userB).Scan(ctx, &held); err != nil {
		t.Fatalf("read grant: %v", err)
	}
	if !held {
		t.Fatal("the local write must land in the database through the claimius functions")
	}
	if err := db.NewRaw(`SELECT EXISTS (SELECT 1 FROM claimius.user_claim WHERE claim_id = ? AND user_id = ? AND sa_deleted_at IS NULL)`, claim.ID, member).Scan(ctx, &held); err != nil {
		t.Fatalf("read member grant: %v", err)
	}
	if !held {
		t.Fatal("the organization user must hold the claim it was created with")
	}
}
