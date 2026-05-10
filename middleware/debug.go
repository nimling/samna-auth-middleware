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
	"golang.org/x/crypto/bcrypt"
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

func ensureDebugUser(ctx context.Context, userID uuid.UUID, debugSecret string) error {
	instance := db.Instance()
	if instance == nil {
		return fmt.Errorf("ensureDebugUser: db not initialized")
	}
	slug := os.Getenv("SAUTH_APP_SLUG")
	if slug == "" {
		return fmt.Errorf("ensureDebugUser: SAUTH_APP_SLUG not set")
	}

	return instance.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewRaw(`SELECT claimius.init_disciple(?)`, slug).Exec(ctx); err != nil {
			return fmt.Errorf("init_disciple: %w", err)
		}

		var userExists bool
		if err := tx.NewRaw(`
			SELECT EXISTS (
				SELECT 1 FROM claimius.samna_user
				WHERE user_id = ?
				  AND app_id = claimius.get_disciple_app_id()
				  AND sa_deleted_at IS NULL
			)
		`, userID).Scan(ctx, &userExists); err != nil {
			return fmt.Errorf("probe samna_user: %w", err)
		}

		var orgID uuid.UUID

		if !userExists {
			preOrgID := uuid.New()
			orgID = preOrgID

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
				int(db.ACCESS_ALL), preOrgID, preOrgID, userID,
			).Scan(ctx, &claimID); err != nil {
				return fmt.Errorf("create_claim: %w", err)
			}

			if _, err := tx.NewRaw(`
				SELECT claimius.assign_claim_object(
					p_app_id        := claimius.get_disciple_app_id(),
					p_claim_id      := ?,
					p_sa_owner_id   := ?,
					p_sa_root_id    := ?,
					p_sa_created_by := ?,
					p_object_id     := ?,
					p_object_type   := ?
				)
			`, claimID, preOrgID, preOrgID, userID, preOrgID, db.OBJECT_TYPE_ORGANIZATION,
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
		} else {
			if err := tx.NewRaw(`
				SELECT id FROM claimius.organization
				 WHERE app_id = claimius.get_disciple_app_id()
				   AND name = 'Debug Organization'
				   AND sa_level = 0
				   AND sa_deleted_at IS NULL
				 LIMIT 1
			`).Scan(ctx, &orgID); err != nil {
				return fmt.Errorf("lookup debug organization: %w", err)
			}
		}

		if debugSecret == "" {
			return nil
		}

		secretHash, err := bcrypt.GenerateFromPassword([]byte(debugSecret), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash debug secret: %w", err)
		}

		if _, err := tx.NewRaw(`
			INSERT INTO claimius.samna_client (
				id, client_id, name, app_id, secret_hash, sa_owner_id, sa_created_by
			) VALUES (
				?, ?, ?, claimius.get_disciple_app_id(), ?, ?, ?
			)
			ON CONFLICT (id) DO UPDATE SET
				secret_hash   = EXCLUDED.secret_hash,
				sa_updated_at = now()
		`, userID, userID, "Debug Client", string(secretHash), orgID, userID,
		).Exec(ctx); err != nil {
			return fmt.Errorf("upsert samna_client: %w", err)
		}

		return nil
	})
}
