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
	tokensBefore := countRows(t, db, "claimius.user_identity")

	claim, err := db.CreateClaim(ctx, nil, ac, sdb.CreateClaimParams{Name: "Routed claim", Access: sdb.ACCESS_READ})
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
	if _, err := db.CreateVirtualClaim(ctx, nil, ac, sdb.CreateVirtualClaimParams{
		Name: "Routed share", Access: sdb.ACCESS_READ, OwnerID: orgRoot,
		ObjectID: orgAlpha, ObjectType: string(sdb.OBJECT_TYPE_ORGANIZATION), TargetUserID: userB,
	}); err != nil {
		t.Fatalf("create virtual claim: %v", err)
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "routed-identity"}).SignedString([]byte("test"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	_, created, err := db.PostUserFromToken(ctx, nil, ac, sdb.SamnaUserToken{Token: token, OwnerID: &orgRoot, ClaimID: ac.ClaimID})
	if err != nil {
		t.Fatalf("issue user token: %v", err)
	}
	if !created {
		t.Fatal("a routed token write must answer created from the prophet's 201")
	}
	if _, err := db.CreateOrganizationUser(ctx, nil, ac, &sdb.SamnaUser{UserID: uuid.New(), Email: "routed@example.com", OwnerID: &orgRoot, ClaimID: &claim.ID}); err != nil {
		t.Fatalf("create organization user: %v", err)
	}
	if _, err := db.DeleteUser(ctx, nil, ac, userB); err != nil {
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
	if after := countRows(t, db, "claimius.user_identity"); after != tokensBefore {
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

	claim, err := db.CreateClaim(ctx, nil, ac, sdb.CreateClaimParams{Name: "Local claim", Access: sdb.ACCESS_READ})
	if err != nil {
		t.Fatalf("create claim: %v", err)
	}
	leveled, err := db.CreateClaim(ctx, nil, ac, sdb.CreateClaimParams{Name: "Leveled claim", Access: sdb.ACCESS_READ, Level: 3})
	if err != nil {
		t.Fatalf("create leveled claim: %v", err)
	}
	var stored int
	if err := db.NewRaw(`SELECT level FROM claimius.claim WHERE id = ?`, leveled.ID).Scan(ctx, &stored); err != nil || leveled.Level != 3 || stored != 3 {
		t.Fatalf("a claim created with level 3 must carry it, got %d stored %d: %v", leveled.Level, stored, err)
	}
	if _, err := claim.AssignUser(userB); err != nil {
		t.Fatalf("assign user: %v", err)
	}
	member := uuid.New()
	if _, err := db.CreateOrganizationUser(ctx, nil, ac, &sdb.SamnaUser{UserID: member, Email: "local@example.com", OwnerID: &orgRoot, ClaimID: &claim.ID}); err != nil {
		t.Fatalf("create organization user: %v", err)
	}
	if again, err := db.CreateOrganizationUser(ctx, nil, ac, &sdb.SamnaUser{UserID: member, OwnerID: &orgRoot, ClaimID: &claim.ID}); err != nil || again.UserID != member {
		t.Fatalf("a second create of the same user upserts its one row and keeps its one grant, got %v %v", again, err)
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "local-identity"}).SignedString([]byte("test"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	first, created, err := db.PostUserFromToken(ctx, nil, ac, sdb.SamnaUserToken{Token: token, OwnerID: &orgRoot, ClaimID: &claim.ID})
	if err != nil || !created {
		t.Fatalf("the first token write must create the user, created %v, err %v", created, err)
	}
	again, created, err := db.PostUserFromToken(ctx, nil, ac, sdb.SamnaUserToken{Token: token, OwnerID: &orgRoot, ClaimID: &claim.ID})
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

func TestARefusedWriteIsErrNoAccessRoutedOrLocal(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	ac, err := db.MutationAccess(ctx, userA, orgRoot, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access: %v", err)
	}

	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"data":null,"count":0,"error":{"message":"cannot grant more access than held"}}`))
	}))
	defer refusing.Close()
	t.Setenv("SAUTH_ENDPOINT", refusing.URL)
	db.RouteToProphet = true
	if _, err := db.CreateClaim(ctx, nil, ac, sdb.CreateClaimParams{Name: "Refused routed", Access: sdb.ACCESS_READ}); !errors.Is(err, sdb.ErrNoAccess) {
		t.Fatalf("a write the prophet answers 403 must be ErrNoAccess, got %v", err)
	}

	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"data":null,"count":0,"error":{"message":"Not Found"}}`))
	}))
	defer missing.Close()
	t.Setenv("SAUTH_ENDPOINT", missing.URL)
	objectID := uuid.New()
	if _, err := db.NewDelete().Model(&sdb.ClaimObject{ID: objectID, ClaimID: uuid.New()}).Access(ac).Exec(ctx); !errors.Is(err, sdb.ErrNotFound) {
		t.Fatalf("a delete the prophet answers 404 must be ErrNotFound, got %v", err)
	}

	db.RouteToProphet = false
	claim, err := db.CreateClaim(ctx, nil, ac, sdb.CreateClaimParams{Name: "Level 5 grant", Access: sdb.ACCESS_READ, Level: 5})
	if err != nil {
		t.Fatalf("create level 5 claim: %v", err)
	}
	organization := sdb.ObjectType(sdb.OBJECT_TYPE_ORGANIZATION)
	if _, err := claim.AssignObject(&sdb.ClaimObject{ObjectID: &orgAlpha, ObjectType: &organization, Inherits: true, OwnerID: orgRoot, RootID: orgRoot}); err != nil {
		t.Fatalf("a binding of a level 5 claim needs no level on the object, got %v", err)
	}
}

func recordingProphet(t *testing.T) *[]string {
	t.Helper()
	var mu sync.Mutex
	calls := make([]string, 0)
	prophet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"data":null,"count":0,"error":{"message":"Not Found"}}`))
	}))
	t.Cleanup(prophet.Close)
	t.Setenv("SAUTH_ENDPOINT", prophet.URL)
	return &calls
}

func TestARoutedWriteTakesItsRecordFromTheModelNeverTheWhere(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	calls := recordingProphet(t)
	db.RouteToProphet = true
	defer func() { db.RouteToProphet = false }()

	relationID := uuid.New()
	userID := uuid.New()
	clientID := uuid.New()
	_ = db.NewUpdate().Model(&sdb.UserRelation{ID: relationID, UserID: userID}).
		Where("user_relation.user_id = ?", userID).
		Where("user_relation.id = ?", relationID).
		Scan(ctx)
	_, _ = db.NewDelete().Model(&sdb.UserRelation{ID: relationID}).Exec(ctx)
	_, _ = db.NewDelete().Model(&sdb.SamnaUser{UserID: userID}).Exec(ctx)
	_, _ = db.NewDelete().Model(&sdb.SamnaClient{ID: clientID}).Exec(ctx)
	want := []string{
		"PATCH /relation/" + relationID.String(), "DELETE /relation/" + relationID.String(),
		"DELETE /user/" + userID.String(), "DELETE /samna_client/" + clientID.String(),
	}
	if len(*calls) != len(want) {
		t.Fatalf("want %v, got %v", want, *calls)
	}
	for i := range want {
		if strings.Replace((*calls)[i], " /sam/", " /", 1) != want[i] {
			t.Fatalf("write %d: want %s, got %s", i, want[i], (*calls)[i])
		}
	}

	*calls = (*calls)[:0]
	for _, where := range []string{"id = ?", "user_relation.id = ?", `"user_relation"."id" = ?`} {
		if err := db.NewUpdate().Model(&sdb.UserRelation{}).Where(where, relationID).Scan(ctx); !errors.Is(err, sdb.ErrRoutedNoRecord) {
			t.Fatalf("update where %q on an empty model: want ErrRoutedNoRecord, got %v", where, err)
		}
		if _, err := db.NewDelete().Model(&sdb.UserRelation{}).Where(where, relationID).Exec(ctx); !errors.Is(err, sdb.ErrRoutedNoRecord) {
			t.Fatalf("delete where %q on an empty model: want ErrRoutedNoRecord, got %v", where, err)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("a write without a record must not reach the prophet, got %v", *calls)
	}
}

func TestARoutedUpdateRefusesASetExpression(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	calls := recordingProphet(t)
	db.RouteToProphet = true
	defer func() { db.RouteToProphet = false }()

	_, err := db.NewUpdate().Model(&sdb.UserRelation{ID: uuid.New()}).
		Set("sa_deleted_at = NOW()").
		Exec(ctx)
	if !errors.Is(err, sdb.ErrRoutedSet) {
		t.Fatalf("a routed update with a Set expression must be ErrRoutedSet, got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("a refused routed update must not reach the prophet, got %v", *calls)
	}
}

func TestALocalBindingJoinsTheCallersTransaction(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	ac, err := db.MutationAccess(ctx, userA, orgRoot, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	orgID := uuid.New()
	organization := sdb.ObjectType(sdb.OBJECT_TYPE_ORGANIZATION)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
		VALUES (?, 'Uncommitted', 'company', ?, ?, 1, ?)`, orgID, orgRoot, orgRoot, *ac.UserClaimID); err != nil {
		t.Fatalf("insert the row in the transaction: %v", err)
	}

	binding := &sdb.ClaimObject{ClaimID: *ac.ClaimID, ObjectID: &orgID, ObjectType: &organization, Inherits: true}
	if err := db.NewInsert().Model(binding).Access(ac).Conn(tx).Scan(ctx); err != nil {
		t.Fatalf("a binding on a row of the caller's uncommitted transaction must succeed on that transaction, got %v", err)
	}
	var inside int
	if err := tx.NewRaw("SELECT count(*) FROM claimius.claim_object WHERE object_id = ? AND sa_deleted_at IS NULL", orgID).Scan(ctx, &inside); err != nil {
		t.Fatalf("count inside: %v", err)
	}
	if inside != 1 {
		t.Fatalf("the binding must be visible inside the transaction, got %d rows", inside)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	var after int
	if err := db.NewRaw("SELECT count(*) FROM claimius.claim_object WHERE object_id = ?", orgID).Scan(ctx, &after); err != nil {
		t.Fatalf("count after: %v", err)
	}
	if after != 0 {
		t.Fatalf("the binding must roll back with the caller's transaction, %d rows left", after)
	}
}

func TestARoutedAnswerAppliesOnTheCallersTransaction(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	ac, err := db.MutationAccess(ctx, userA, orgRoot, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	calls, stop := stubProphet(t, db)
	defer stop()
	db.RouteToProphet = true
	defer func() { db.RouteToProphet = false }()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	relationID := uuid.New()
	relation := &sdb.UserRelation{ID: relationID, UserID: userA, ObjectType: sdb.OBJECT_TYPE_ORGANIZATION, ObjectID: orgRoot}
	if err := db.NewInsert().Model(relation).Access(ac).Conn(tx).Scan(ctx); err != nil {
		t.Fatalf("routed insert: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0].method != http.MethodPost {
		t.Fatalf("the write must still go to the prophet once, got %v", *calls)
	}
	var inside int
	if err := tx.NewRaw("SELECT count(*) FROM claimius.user_relation WHERE id = ?", relationID).Scan(ctx, &inside); err != nil {
		t.Fatalf("count inside: %v", err)
	}
	if inside != 1 {
		t.Fatalf("the prophet's answer must be applied on the caller's transaction, got %d rows", inside)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	var after int
	if err := db.NewRaw("SELECT count(*) FROM claimius.user_relation WHERE id = ?", relationID).Scan(ctx, &after); err != nil {
		t.Fatalf("count after: %v", err)
	}
	if after != 0 {
		t.Fatalf("the local copy must roll back with the caller's transaction, %d rows left", after)
	}
}

func TestCreateVirtualClaimCommitsAndRollsBackWithTheCallersTransaction(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	db.RouteToProphet = false
	ac, err := db.MutationAccess(ctx, userA, orgRoot, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	for _, commit := range []bool{false, true} {
		name := "Tx claim " + uuid.NewString()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		granted, err := db.CreateVirtualClaim(ctx, tx, ac, sdb.CreateVirtualClaimParams{
			Name:         name,
			Access:       sdb.ACCESS_READ,
			OwnerID:      orgRoot,
			ObjectID:     orgAlpha,
			ObjectType:   sdb.OBJECT_TYPE_ORGANIZATION,
			TargetUserID: userB,
		})
		if err != nil {
			tx.Rollback()
			t.Fatalf("create virtual claim: %v", err)
		}
		var inside, outside int
		if err := tx.NewRaw(`SELECT count(*) FROM claimius.user_claim WHERE id = ? AND sa_deleted_at IS NULL`, *granted.UserClaimID).Scan(ctx, &inside); err != nil {
			tx.Rollback()
			t.Fatalf("read inside: %v", err)
		}
		if err := db.NewRaw(`SELECT count(*) FROM claimius.claim WHERE name = ?`, name).Scan(ctx, &outside); err != nil {
			tx.Rollback()
			t.Fatalf("read outside: %v", err)
		}
		if inside != 1 || outside != 0 {
			tx.Rollback()
			t.Fatalf("the write must sit only in the caller's transaction, inside %d outside %d", inside, outside)
		}
		if commit {
			err = tx.Commit()
		} else {
			err = tx.Rollback()
		}
		if err != nil {
			t.Fatalf("end transaction: %v", err)
		}
		var claims, grants, bindings int
		if err := db.NewRaw(`SELECT count(*) FROM claimius.claim WHERE name = ?`, name).Scan(ctx, &claims); err != nil {
			t.Fatalf("read claim: %v", err)
		}
		if err := db.NewRaw(`SELECT count(*) FROM claimius.user_claim WHERE claim_id = ?`, *granted.ClaimID).Scan(ctx, &grants); err != nil {
			t.Fatalf("read grant: %v", err)
		}
		if err := db.NewRaw(`SELECT count(*) FROM claimius.claim_object WHERE claim_id = ?`, *granted.ClaimID).Scan(ctx, &bindings); err != nil {
			t.Fatalf("read binding: %v", err)
		}
		want := 0
		if commit {
			want = 1
		}
		if claims != want || grants != want || bindings != want {
			t.Fatalf("commit %v: claim %d grant %d binding %d, want %d each", commit, claims, grants, bindings, want)
		}
	}
}

func TestARoutedUpdateSendsOnlyTheColumnsTheQueryKeeps(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	var sent map[string]any
	prophet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		sent = map[string]any{}
		_ = json.Unmarshal(raw, &sent)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":null,"count":0}`))
	}))
	defer prophet.Close()
	t.Setenv("SAUTH_ENDPOINT", prophet.URL)
	db.RouteToProphet = true
	defer func() { db.RouteToProphet = false }()

	relationID := uuid.New()
	description := "updated"
	_ = db.NewUpdate().Model(&sdb.UserRelation{ID: relationID, UserID: uuid.New(), Description: &description, ObjectID: uuid.New(), ObjectType: "claimius.organization"}).
		ExcludeColumn("id", "user_id", "used_at", "sa_created_at", "sa_updated_at", "sa_deleted_at").
		OmitZero().
		Scan(ctx)
	for _, key := range []string{"id", "user_id", "priority", "pinned"} {
		if _, ok := sent[key]; ok {
			t.Fatalf("a routed update must not send %q, an excluded or zero column, got %v", key, sent)
		}
	}
	if sent["description"] != description || sent["object_type"] == nil {
		t.Fatalf("a routed update must send the columns it keeps, got %v", sent)
	}
}

func TestARoutedAnswerKeepsTheColumnsItDoesNotCarry(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	clientID := uuid.New()
	if _, err := db.ExecContext(ctx, `INSERT INTO claimius.samna_client (id, name, client_id, secret_hash, status, sa_owner_id) VALUES (?, 'Machine', ?, 'kept', 'active', ?)`,
		clientID, uuid.New(), uuid.New()); err != nil {
		t.Fatalf("client: %v", err)
	}
	prophet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		row := map[string]any{"id": clientID.String(), "name": "Renamed", "status": "active"}
		w.Header().Set("Content-Type", "application/json")
		body, _ := json.Marshal(map[string]any{"data": row, "count": 1})
		w.Write(body)
	}))
	defer prophet.Close()
	t.Setenv("SAUTH_ENDPOINT", prophet.URL)
	db.RouteToProphet = true
	defer func() { db.RouteToProphet = false }()

	if err := db.NewUpdate().Model(&sdb.SamnaClient{ID: clientID, Name: "Renamed"}).Column("name").Scan(ctx); err != nil {
		t.Fatalf("routed update: %v", err)
	}
	var stored struct {
		Name       string `bun:"name"`
		SecretHash string `bun:"secret_hash"`
	}
	if err := db.NewRaw(`SELECT name, secret_hash FROM claimius.samna_client WHERE id = ?`, clientID).Scan(ctx, &stored); err != nil {
		t.Fatalf("read client: %v", err)
	}
	if stored.Name != "Renamed" || stored.SecretHash != "kept" {
		t.Fatalf("the local copy of a routed answer must set the columns it carries and keep the ones it does not, got %+v", stored)
	}
}

func TestABindingKeepsTheOwnerItIsGivenRoutedOrLocal(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	ac, err := db.MutationAccess(ctx, userA, orgAlpha, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	if ac.OwnerID != orgRoot {
		t.Fatalf("the access on Alpha must carry Alpha's own owner, Root, got %s", ac.OwnerID)
	}
	organization := sdb.ObjectType(sdb.OBJECT_TYPE_ORGANIZATION)

	var owners []string
	prophet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		row := map[string]any{}
		_ = json.Unmarshal(raw, &row)
		owner, _ := row["owner_id"].(string)
		owners = append(owners, owner)
		row["id"] = uuid.New().String()
		body, _ := json.Marshal(map[string]any{"data": row, "count": 1})
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer prophet.Close()
	t.Setenv("SAUTH_ENDPOINT", prophet.URL)
	db.RouteToProphet = true
	routed := uuid.New()
	if err := db.NewInsert().Model(&sdb.ClaimObject{ClaimID: *ac.ClaimID, ObjectID: &routed, ObjectType: &organization, OwnerID: orgAlpha, Inherits: true}).Access(ac).Scan(ctx); err != nil {
		t.Fatalf("routed binding: %v", err)
	}
	if len(owners) != 1 || owners[0] != orgAlpha.String() {
		t.Fatalf("the routed binding must carry the owner it was given, Alpha, got %v", owners)
	}

	db.RouteToProphet = false
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	local := uuid.New()
	if _, err := tx.ExecContext(ctx, `INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
		VALUES (?, 'Under Alpha', 'company', ?, ?, 2, ?)`, local, orgAlpha, orgRoot, *ac.UserClaimID); err != nil {
		t.Fatalf("insert the row: %v", err)
	}
	if err := db.NewInsert().Model(&sdb.ClaimObject{ClaimID: *ac.ClaimID, ObjectID: &local, ObjectType: &organization, OwnerID: orgAlpha, Inherits: true}).Access(ac).Conn(tx).Scan(ctx); err != nil {
		t.Fatalf("local binding: %v", err)
	}
	var owner uuid.UUID
	if err := tx.NewRaw("SELECT sa_owner_id FROM claimius.claim_object WHERE object_id = ? AND sa_deleted_at IS NULL", local).Scan(ctx, &owner); err != nil {
		t.Fatalf("read the binding: %v", err)
	}
	if owner != orgAlpha {
		t.Fatalf("the local binding must keep the owner it was given, Alpha, got %s", owner)
	}

	unset := uuid.New()
	if _, err := tx.ExecContext(ctx, `INSERT INTO claimius.organization (id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
		VALUES (?, 'Unset under Alpha', 'company', ?, ?, 2, ?)`, unset, orgAlpha, orgRoot, *ac.UserClaimID); err != nil {
		t.Fatalf("insert the row: %v", err)
	}
	if err := db.NewInsert().Model(&sdb.ClaimObject{ClaimID: *ac.ClaimID, ObjectID: &unset, ObjectType: &organization, Inherits: true}).Access(ac).Conn(tx).Scan(ctx); err != nil {
		t.Fatalf("local binding without an owner: %v", err)
	}
	if err := tx.NewRaw("SELECT sa_owner_id FROM claimius.claim_object WHERE object_id = ? AND sa_deleted_at IS NULL", unset).Scan(ctx, &owner); err != nil {
		t.Fatalf("read the binding: %v", err)
	}
	if owner != ac.OwnerID {
		t.Fatalf("a binding given no owner takes the access owner %s, got %s", ac.OwnerID, owner)
	}
}

func TestARoutedOrganizationMoveIsCheckedBeforeTheProphet(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	ac, err := db.MutationAccess(ctx, userA, orgGrand, sdb.OBJECT_TYPE_ORGANIZATION, sdb.ACCESS_WRITE)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	calls := recordingProphet(t)
	db.RouteToProphet = true
	defer func() { db.RouteToProphet = false }()

	cycle := sdb.Organization{ID: orgAlpha, OwnerID: orgGrand}
	if err := db.NewUpdate().Model(&cycle).Access(ac).OmitZero().Scan(ctx, &cycle); !errors.Is(err, sdb.ErrOrganizationParent) {
		t.Fatalf("a routed move under a descendant must be refused, got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("a refused move must not reach the prophet, got %v", *calls)
	}
	move := sdb.Organization{ID: orgGrand, OwnerID: orgRoot}
	_ = db.NewUpdate().Model(&move).Access(ac).OmitZero().Scan(ctx, &move)
	if len(*calls) != 1 || strings.Replace((*calls)[0], " /sam/", " /", 1) != "PATCH /organization/"+orgGrand.String() {
		t.Fatalf("a routed move must patch the organization on the prophet, got %v", *calls)
	}
}
