//go:build dbtest

package echo_test

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/google/uuid"
	psdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

func postAppSecret(t *testing.T, app peerApp, plain string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	db := psdb.Instance()
	if _, err := db.ExecContext(ctx, `SELECT claimius.upsert_app_user(p_user_id => ?, p_app_id => ?, p_first_name => 'Ada')`, grantUser, app.ID); err != nil {
		t.Fatalf("app user: %v", err)
	}
	claimID := postPeerClaim(t, db, app.ID, app.Org, grantUser, "Secret keeper "+uuid.NewString()[:8])
	var grantID uuid.UUID
	if err := db.NewRaw(`
		SELECT (claimius.insert_user_claim(p_app_id => ?, p_claim_id => ?, p_user_id => ?, p_sa_owner_id => ?, p_sa_created_by => ?) -> 'user_claim' ->> 'id')::UUID
	`, app.ID, claimID, grantUser, app.Org, grantUser).Scan(ctx, &grantID); err != nil {
		t.Fatalf("grant: %v", err)
	}
	seed, err := hex.DecodeString(app.Seed)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	sealed, err := utils.AesEncrypt(plain, seed)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	var id uuid.UUID
	if err := db.NewRaw(`SELECT (claimius.insert_secret(?, ?, ?, NULL, ?, ?) -> 'samna_secret' ->> 'id')::uuid`,
		app.ID, "gotest_secret_"+uuid.NewString()[:8], hex.EncodeToString(sealed), app.Org, grantID).Scan(ctx, &id); err != nil {
		t.Fatalf("insert secret: %v", err)
	}
	return id
}

func TestResolveOpensASecretWithTheSeedOfItsOwnApp(t *testing.T) {
	getGrantFixture(t)
	app := postPeerApp(t, grantUser)
	secretID := postAppSecret(t, app, "s3cret")
	ctx := context.Background()
	db := psdb.Instance()
	stored, err := db.GetSecret(ctx, secretID)
	if err != nil {
		t.Fatalf("read secret: %v", err)
	}

	plain, err := stored.Resolve(ctx, db, grantUser)
	if err != nil || plain != "s3cret" {
		t.Fatalf("the creator must open a secret on the prophet with the seed of the app that wrote it, not the system app's, got %q, %v", plain, err)
	}
	if _, err := stored.Resolve(ctx, db, uuid.New()); !errors.Is(err, shared.ErrNoAccess) {
		t.Fatalf("a user without read must be refused before any seed is used, got %v", err)
	}
	var appID uuid.UUID
	if err := db.NewRaw(`SELECT app_id FROM claimius.get_secret_value(?, ?)`, grantUser, secretID).Scan(ctx, &appID); err != nil || appID != app.ID {
		t.Fatalf("get_secret_value must answer the secret's app, got %s, %v", appID, err)
	}
}
