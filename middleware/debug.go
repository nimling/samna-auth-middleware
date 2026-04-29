package middleware

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/uptrace/bun"
)

func generateDebugMasterKey() (string, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return "", "", fmt.Errorf("generate rsa key: %w", err)
	}
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return "", "", fmt.Errorf("generate seed: %w", err)
	}
	privateKeyBase64 := base64.RawURLEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(privateKey))
	seedHex := hex.EncodeToString(seed)
	return privateKeyBase64, seedHex, nil
}

// ensureDebugUser idempotently materializes a simulated logged in user.
// If a samna_user row already exists for the configured disciple app, returns
// immediately. Otherwise, in a single transaction, registers the disciple via
// claimius.init_disciple, upserts a samna_app row, creates a self rooted
// organization, an owner claim bound to that organization, the samna_user via
// claimius.ensure_app_user, and a user_claim grant. sa_created_by is the
// user_id under the bootstrap exception described in .claude/rules/claimius.md.
// All app id resolution after the samna_app upsert goes through
// claimius.get_disciple_app_id() inline in SQL.
func ensureDebugUser(ctx context.Context, userID uuid.UUID) error {
	instance := db.Instance()
	if instance == nil {
		return fmt.Errorf("ensureDebugUser: db not initialized")
	}
	slug := os.Getenv("SAUTH_APP_SLUG")
	if slug == "" {
		return fmt.Errorf("ensureDebugUser: SAUTH_APP_SLUG not set")
	}

	return instance.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// Register the disciple; sets disciple_state.disciple_app_slug so
		// claimius.get_disciple_app_id() returns the id for this slug.
		if _, err := tx.NewRaw(`SELECT claimius.init_disciple(?)`, slug).Exec(ctx); err != nil {
			return fmt.Errorf("init_disciple: %w", err)
		}

		// Probe: if the samna_user already exists for the disciple app, no work.
		var exists bool
		if err := tx.NewRaw(`
			SELECT EXISTS (
				SELECT 1 FROM claimius.samna_user
				WHERE user_id = ?
				  AND app_id = claimius.get_disciple_app_id()
				  AND sa_deleted_at IS NULL
			)
		`, userID).Scan(ctx, &exists); err != nil {
			return fmt.Errorf("probe samna_user: %w", err)
		}
		if exists {
			return nil
		}

		preOrgID := uuid.New()

		privateKey, privateSeed, err := generateDebugMasterKey()
		if err != nil {
			return fmt.Errorf("generate master key: %w", err)
		}

		if _, err := tx.NewRaw(`
			INSERT INTO claimius.samna_app (slug, name, description, private_key, private_seed, sa_owner_id, sa_created_by)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (slug) DO UPDATE SET
				private_key   = CASE WHEN claimius.samna_app.private_key   = '' THEN EXCLUDED.private_key   ELSE claimius.samna_app.private_key   END,
				private_seed  = CASE WHEN claimius.samna_app.private_seed  = '' THEN EXCLUDED.private_seed  ELSE claimius.samna_app.private_seed  END,
				sa_updated_at = now()
		`, slug, "Debug App", "Debug bootstrap app", privateKey, privateSeed, preOrgID, userID,
		).Exec(ctx); err != nil {
			return fmt.Errorf("upsert samna_app: %w", err)
		}

		if _, err := tx.NewRaw(`
			INSERT INTO claimius.organization (id, app_id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
			VALUES (?, claimius.get_disciple_app_id(), ?, ?, ?, ?, ?, ?)
		`, preOrgID, "Debug Organization", string(db.ORGANIZATION_TYPE_COMPANY),
			preOrgID, preOrgID, 0, userID,
		).Exec(ctx); err != nil {
			return fmt.Errorf("insert organization: %w", err)
		}

		if _, err := tx.NewRaw(`
			SELECT claimius.ensure_app_user(
				p_user_id := ?,
				p_app_id := claimius.get_disciple_app_id(),
				p_first_name := ?, p_last_name := ?, p_email := ?
			)
		`, userID, "Debug", "User", "debug@local",
		).Exec(ctx); err != nil {
			return fmt.Errorf("ensure_app_user: %w", err)
		}

		var claimID uuid.UUID
		if err := tx.NewRaw(`
			SELECT (claimius.create_claim(
				claimius.get_disciple_app_id(),
				?, ?, ?, ?, ?, ?
			) -> 'claim' ->> 'id')::uuid
		`, "Owner", "Owner of debug organization",
			int(db.ACCESS_LEVEL_OWNER), preOrgID, preOrgID, userID,
		).Scan(ctx, &claimID); err != nil {
			return fmt.Errorf("create_claim: %w", err)
		}

		if _, err := tx.NewRaw(`
			SELECT claimius.assign_claim_object(
				claimius.get_disciple_app_id(),
				?, ?, ?, ?, ?, ?
			)
		`, claimID, preOrgID, db.OBJECT_TYPE_ORGANIZATION,
			preOrgID, preOrgID, userID,
		).Exec(ctx); err != nil {
			return fmt.Errorf("assign_claim_object: %w", err)
		}

		if _, err := tx.NewRaw(`
			SELECT claimius.assign_claim_user(
				claimius.get_disciple_app_id(),
				?, ?, ?, ?
			)
		`, claimID, userID, preOrgID, userID,
		).Exec(ctx); err != nil {
			return fmt.Errorf("assign_claim_user: %w", err)
		}

		return nil
	})
}
