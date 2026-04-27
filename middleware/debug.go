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
// If a samna_user row already exists for the configured slug, returns
// immediately. Otherwise, in a single transaction, upserts a samna_app row,
// creates the samna_user via claimius.ensure_app_user, a self rooted
// organization, an owner claim bound to that organization, and a user_claim
// grant attaching the claim to the user. sa_created_by is the user_id under
// the bootstrap exception described in .claude/rules/claimius.md.
func ensureDebugUser(ctx context.Context, userID uuid.UUID) error {
	instance := db.Instance()
	if instance == nil {
		return fmt.Errorf("ensureDebugUser: db not initialized")
	}
	slug := os.Getenv("SAUTH_APP_SLUG")
	if slug == "" {
		return fmt.Errorf("ensureDebugUser: SAUTH_APP_SLUG not set")
	}

	exists, err := instance.NewSelect().
		Model((*db.SamnaUser)(nil)).
		Where("user_id = ?", userID).
		Where("app_id = (SELECT id FROM claimius.samna_app WHERE slug = ? AND sa_deleted_at IS NULL)", slug).
		Where("sa_deleted_at IS NULL").
		Exists(ctx)
	if err != nil {
		return fmt.Errorf("probe samna_user: %w", err)
	}
	if exists {
		return nil
	}

	return instance.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		preOrgID := uuid.New()

		var appID uuid.UUID
		if err := tx.NewRaw(`
			INSERT INTO claimius.samna_app (slug, name, description, private_key, private_seed, sa_owner_id, sa_created_by)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (slug) DO UPDATE SET sa_updated_at = now()
			RETURNING id
		`, slug, "Debug App", "Debug bootstrap app", "", "", preOrgID, userID,
		).Scan(ctx, &appID); err != nil {
			return fmt.Errorf("upsert samna_app: %w", err)
		}

		org := db.Organization{
			ID:        preOrgID,
			AppID:     appID,
			Name:      "Debug Organization",
			Type:      db.ORGANIZATION_TYPE_COMPANY,
			OwnerID:   preOrgID,
			RootID:    preOrgID,
			Level:     0,
			CreatedBy: userID,
		}
		if _, err := tx.NewInsert().Model(&org).Exec(ctx); err != nil {
			return fmt.Errorf("insert organization: %w", err)
		}

		if _, err := tx.NewRaw(
			`SELECT claimius.ensure_app_user(
				p_user_id := ?, p_app_id := ?,
				p_first_name := ?, p_last_name := ?, p_email := ?
			)`,
			userID, appID, "Debug", "User", "debug@local",
		).Exec(ctx); err != nil {
			return fmt.Errorf("ensure_app_user: %w", err)
		}

		var claimRaw json.RawMessage
		if err := tx.NewRaw(
			`SELECT claimius.create_claim(?, ?, ?, ?, ?, ?, ?) AS payload`,
			appID, "Owner", "Owner of debug organization",
			int(db.ACCESS_LEVEL_OWNER), preOrgID, preOrgID, userID,
		).Scan(ctx, &claimRaw); err != nil {
			return fmt.Errorf("create_claim: %w", err)
		}
		var claimWrap struct {
			Claim struct {
				ID uuid.UUID `json:"id"`
			} `json:"claim"`
		}
		if err := json.Unmarshal(claimRaw, &claimWrap); err != nil {
			return fmt.Errorf("parse create_claim: %w", err)
		}
		claimID := claimWrap.Claim.ID

		if _, err := tx.NewRaw(
			`SELECT claimius.assign_claim_object(?, ?, ?, ?, ?, ?, ?)`,
			appID, claimID, preOrgID, db.OBJECT_TYPE_ORGANIZATION,
			preOrgID, preOrgID, userID,
		).Exec(ctx); err != nil {
			return fmt.Errorf("assign_claim_object: %w", err)
		}

		if _, err := tx.NewRaw(
			`SELECT claimius.assign_claim_user(?, ?, ?, ?, ?)`,
			appID, claimID, userID, preOrgID, userID,
		).Exec(ctx); err != nil {
			return fmt.Errorf("assign_claim_user: %w", err)
		}

		return nil
	})
}
