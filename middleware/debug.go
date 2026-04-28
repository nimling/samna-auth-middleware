package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/uptrace/bun"
)

// ensureDebugUser idempotently materializes a simulated logged in user.
// If a samna_user row already exists for the configured disciple app, returns
// immediately. Otherwise, in a single transaction, registers the disciple via
// claimius.init_disciple, upserts a samna_app row, creates a self rooted
// organization, an owner claim bound to that organization, the samna_user via
// claimius.ensure_app_user, and a user_claim grant. sa_created_by is the
// user_id under the bootstrap exception described in .claude/rules/claimius.md.
// All app id resolution after the samna_app upsert goes through
// claimius.get_disciple_app() inline in SQL.
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
		// claimius.get_disciple_app() returns the row for this slug.
		if _, err := tx.NewRaw(`SELECT claimius.init_disciple(?)`, slug).Exec(ctx); err != nil {
			return fmt.Errorf("init_disciple: %w", err)
		}

		// Probe: if the samna_user already exists for the disciple app, no work.
		var exists bool
		if err := tx.NewRaw(`
			SELECT EXISTS (
				SELECT 1 FROM claimius.samna_user
				WHERE user_id = ?
				  AND app_id = (SELECT id FROM claimius.get_disciple_app() LIMIT 1)
				  AND sa_deleted_at IS NULL
			)
		`, userID).Scan(ctx, &exists); err != nil {
			return fmt.Errorf("probe samna_user: %w", err)
		}
		if exists {
			return nil
		}

		preOrgID := uuid.New()

		// Upsert samna_app. After this, claimius.get_disciple_app() resolves to
		// this row and is the canonical source of app_id everywhere downstream.
		if _, err := tx.NewRaw(`
			INSERT INTO claimius.samna_app (slug, name, description, private_key, private_seed, sa_owner_id, sa_created_by)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (slug) DO UPDATE SET sa_updated_at = now()
		`, slug, "Debug App", "Debug bootstrap app", "", "", preOrgID, userID,
		).Exec(ctx); err != nil {
			return fmt.Errorf("upsert samna_app: %w", err)
		}

		// Insert organization with app_id resolved via get_disciple_app().
		if _, err := tx.NewRaw(`
			INSERT INTO claimius.organization (id, app_id, name, type, sa_owner_id, sa_root_id, sa_level, sa_created_by)
			VALUES (?, (SELECT id FROM claimius.get_disciple_app() LIMIT 1), ?, ?, ?, ?, ?, ?)
		`, preOrgID, "Debug Organization", string(db.ORGANIZATION_TYPE_COMPANY),
			preOrgID, preOrgID, 0, userID,
		).Exec(ctx); err != nil {
			return fmt.Errorf("insert organization: %w", err)
		}

		if _, err := tx.NewRaw(`
			SELECT claimius.ensure_app_user(
				p_user_id := ?,
				p_app_id := (SELECT id FROM claimius.get_disciple_app() LIMIT 1),
				p_first_name := ?, p_last_name := ?, p_email := ?
			)
		`, userID, "Debug", "User", "debug@local",
		).Exec(ctx); err != nil {
			return fmt.Errorf("ensure_app_user: %w", err)
		}

		var claimRow struct {
			Payload json.RawMessage `bun:"payload"`
		}
		if err := tx.NewRaw(`
			SELECT claimius.create_claim(
				(SELECT id FROM claimius.get_disciple_app() LIMIT 1),
				?, ?, ?, ?, ?, ?
			) AS payload
		`, "Owner", "Owner of debug organization",
			int(db.ACCESS_LEVEL_OWNER), preOrgID, preOrgID, userID,
		).Scan(ctx, &claimRow); err != nil {
			return fmt.Errorf("create_claim: %w", err)
		}
		var claimWrap struct {
			Claim struct {
				ID uuid.UUID `json:"id"`
			} `json:"claim"`
		}
		if err := json.Unmarshal(claimRow.Payload, &claimWrap); err != nil {
			return fmt.Errorf("parse create_claim: %w", err)
		}
		claimID := claimWrap.Claim.ID

		if _, err := tx.NewRaw(`
			SELECT claimius.assign_claim_object(
				(SELECT id FROM claimius.get_disciple_app() LIMIT 1),
				?, ?, ?, ?, ?, ?
			)
		`, claimID, preOrgID, db.OBJECT_TYPE_ORGANIZATION,
			preOrgID, preOrgID, userID,
		).Exec(ctx); err != nil {
			return fmt.Errorf("assign_claim_object: %w", err)
		}

		if _, err := tx.NewRaw(`
			SELECT claimius.assign_claim_user(
				(SELECT id FROM claimius.get_disciple_app() LIMIT 1),
				?, ?, ?, ?
			)
		`, claimID, userID, preOrgID, userID,
		).Exec(ctx); err != nil {
			return fmt.Errorf("assign_claim_user: %w", err)
		}

		return nil
	})
}
