//go:build dbtest

package shared_test

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/v2/client"
	sdb "github.com/nimling/samna-auth-middleware/v2/shared"
)

type seedEnvelope struct {
	Data  sdb.AppSeed `json:"data"`
	Count int         `json:"count"`
}

func getSecretSeed(t *testing.T) []byte {
	t.Helper()
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return seed
}

func getSealedValue(t *testing.T, plain string, seed []byte) string {
	t.Helper()
	block, err := aes.NewCipher(seed)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("nonce: %v", err)
	}
	return hex.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil))
}

func postKeptSecret(t *testing.T, db *sdb.DB, value string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var creator uuid.UUID
	if err := db.NewRaw(`
		SELECT uc.id FROM claimius.user_claim uc JOIN claimius.claim c ON c.id = uc.claim_id
		WHERE c.name = 'Kept owner' AND uc.user_id = ? AND uc.sa_deleted_at IS NULL
	`, userC).Scan(ctx, &creator); err != nil {
		t.Fatalf("creator grant: %v", err)
	}
	var id uuid.UUID
	if err := db.NewRaw(`SELECT (claimius.insert_secret(?, ?, NULL, ?, ?) -> 'samna_secret' ->> 'id')::uuid`,
		"gotest_secret_"+uuid.NewString()[:8], value, orgKept, creator).Scan(ctx, &id); err != nil {
		t.Fatalf("insert secret: %v", err)
	}
	return id
}

func postSecretReader(t *testing.T, db *sdb.DB, secretID, userID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	var claimID uuid.UUID
	if err := db.NewRaw(`
		SELECT (claimius.insert_claim(p_name => ?, p_description => NULL, p_sa_access => 4,
		        p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?) -> 'claim' ->> 'id')::UUID
	`, "Secret reader "+secretID.String()[:8], orgKept, orgKept, userC).Scan(ctx, &claimID); err != nil {
		t.Fatalf("reader claim: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		SELECT claimius.insert_claim_object(p_claim_id => ?, p_sa_owner_id => ?, p_sa_root_id => ?, p_sa_created_by => ?,
		       p_object_id => ?, p_object_type => 'claimius.samna_secret', p_inherits => TRUE)
	`, claimID, orgKept, orgKept, userC, secretID); err != nil {
		t.Fatalf("bind reader claim: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		SELECT claimius.insert_user_claim(p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?)
	`, claimID, userID, orgKept, userC); err != nil {
		t.Fatalf("grant reader claim: %v", err)
	}
}

func getSecretValue(t *testing.T, db *sdb.DB, userID, secretID uuid.UUID) sql.NullString {
	t.Helper()
	var value sql.NullString
	if err := db.NewRaw(`SELECT claimius.get_secret_value(?, ?)`, userID, secretID).Scan(context.Background(), &value); err != nil {
		t.Fatalf("get_secret_value: %v", err)
	}
	return value
}

func TestGetSecretValueAnswersOnlyAReader(t *testing.T) {
	db := database(t)
	secretID := postKeptSecret(t, db, "cipher")
	postSecretReader(t, db, secretID, userB)

	if v := getSecretValue(t, db, userC, secretID); !v.Valid || v.String != "cipher" {
		t.Fatalf("the creator must read the stored ciphertext through its bound claim, got %+v", v)
	}
	if v := getSecretValue(t, db, userB, secretID); !v.Valid || v.String != "cipher" {
		t.Fatalf("a holder of a claim bound to the secret must read it, got %+v", v)
	}
	if v := getSecretValue(t, db, userA, secretID); v.Valid {
		t.Fatalf("a user without a claim reaching the secret must read nothing, got %q", v.String)
	}
}

func getProphetSeed(t *testing.T, seeds ...[]byte) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sam/app" || r.Header.Get("SAuth-App-Slug") != "gotest_resolve" || r.Header.Get("SAuth-App-Secret") != "gotest-resolve-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		n := int(calls.Add(1)) - 1
		seed := seeds[min(n, len(seeds)-1)]
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(seedEnvelope{Data: sdb.AppSeed{PrivateSeed: hex.EncodeToString(seed)}, Count: 1})
	}))
	t.Cleanup(server.Close)
	t.Setenv("SAUTH_ENDPOINT", server.URL)
	t.Setenv("SAUTH_APP_SLUG", "gotest_resolve")
	t.Setenv("SAUTH_APP_SECRET", "gotest-resolve-secret")
	t.Setenv("SAUTH_DEBUG_USER_ID", "")
	t.Setenv("SAUTH_DEBUG_CLIENTS", "")
	t.Setenv("SAUTH_DEBUG_PRIVATE_SEED", "")
	client.Bind(server.URL, "")
	sdb.DeleteAppSeed()
	t.Cleanup(sdb.DeleteAppSeed)
	return &calls
}

func getDeployedDisciple(t *testing.T) *sdb.DB {
	t.Helper()
	db := database(t)
	db.RouteToProphet = true
	return db
}

func TestResolveOpensASecretOnADeployedDisciple(t *testing.T) {
	seed := getSecretSeed(t)
	calls := getProphetSeed(t, seed)
	db := getDeployedDisciple(t)
	ctx := context.Background()
	secretID := postKeptSecret(t, db, getSealedValue(t, "s3cret", seed))
	stored, err := db.GetSecret(ctx, secretID)
	if err != nil {
		t.Fatalf("read secret: %v", err)
	}

	plain, err := stored.Resolve(ctx, db, userC)
	if err != nil || plain != "s3cret" {
		t.Fatalf("the creator must open the secret with the seed the prophet delivered, got %q, %v", plain, err)
	}
	if _, err := stored.Resolve(ctx, db, userA); !errors.Is(err, sdb.ErrNoAccess) {
		t.Fatalf("a user without read must be refused before any seed is used, got %v", err)
	}
	if _, err := stored.Resolve(ctx, db, userC); err != nil {
		t.Fatalf("a second resolve: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("the seed must be fetched once and held in process, fetched %d times", calls.Load())
	}
}

func TestResolveFetchesTheSeedAgainWhenItChanged(t *testing.T) {
	stale := getSecretSeed(t)
	seed := getSecretSeed(t)
	calls := getProphetSeed(t, stale, seed)
	db := getDeployedDisciple(t)
	ctx := context.Background()
	if _, err := sdb.GetAppSeed(ctx); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	secretID := postKeptSecret(t, db, getSealedValue(t, "rotated", seed))
	stored, err := db.GetSecret(ctx, secretID)
	if err != nil {
		t.Fatalf("read secret: %v", err)
	}
	plain, err := stored.Resolve(ctx, db, userC)
	if err != nil || plain != "rotated" {
		t.Fatalf("a value the held seed cannot open must be opened with a fresh seed, got %q, %v", plain, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected one refetch, got %d fetches", calls.Load())
	}
}
