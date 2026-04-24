package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nimling/samna-auth-middleware/db"
	"github.com/uptrace/bun"
)

type SyncPayload struct {
	Type string          `json:"type"`
	Seq  int64           `json:"seq"`
	Obj  json.RawMessage `json:"obj"`
}

func ApplyPayload(ctx context.Context, payload SyncPayload) error {
	return db.Instance().RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, "SET LOCAL samna_auth.replay_mode = 'true'"); err != nil {
			return fmt.Errorf("set replay_mode: %w", err)
		}

		switch payload.Type {
		case "organization":
			return applyOrganization(ctx, tx, payload.Obj)
		case "location":
			return applyLocation(ctx, tx, payload.Obj)
		case "samna_user":
			return applySamnaUser(ctx, tx, payload.Obj)
		case "claim":
			return applyClaim(ctx, tx, payload.Obj)
		case "user_claim":
			return applyUserClaim(ctx, tx, payload.Obj)
		case "claim_object":
			return applyClaimObject(ctx, tx, payload.Obj)
		case "samna_client":
			return applySamnaClient(ctx, tx, payload.Obj)
		case "samna_secret":
			return applySamnaSecret(ctx, tx, payload.Obj)
		case "samna_app":
			return applySamnaApp(ctx, tx, payload.Obj)
		case "object_field":
			return applyObjectField(ctx, tx, payload.Obj)
		case "user_field":
			return applyUserField(ctx, tx, payload.Obj)
		case "user_relation":
			return applyUserRelation(ctx, tx, payload.Obj)
		case "audit":
			return applyAudit(ctx, tx, payload.Obj)
		default:
			return fmt.Errorf("unknown sync type: %s", payload.Type)
		}
	})
}

func applySamnaUser(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.SamnaUser
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("first_name = EXCLUDED.first_name").
		Set("last_name = EXCLUDED.last_name").
		Set("phone = EXCLUDED.phone").
		Set("email = EXCLUDED.email").
		Set("user_id = EXCLUDED.user_id").
		Set("status = EXCLUDED.status").
		Set("type = EXCLUDED.type").
		Set("config = EXCLUDED.config").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func applyOrganization(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.Organization
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("name = EXCLUDED.name").
		Set("description = EXCLUDED.description").
		Set("sa_root_id = EXCLUDED.sa_root_id").
		Set("sa_owner_id = EXCLUDED.sa_owner_id").
		Set("type = EXCLUDED.type").
		Set("sa_level = EXCLUDED.sa_level").
		Set("external_id = EXCLUDED.external_id").
		Set("provider_id = EXCLUDED.provider_id").
		Set("sa_created_by = EXCLUDED.sa_created_by").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func applyLocation(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.Location
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("sa_owner_id = EXCLUDED.sa_owner_id").
		Set("sa_root_id = EXCLUDED.sa_root_id").
		Set("name = EXCLUDED.name").
		Set("description = EXCLUDED.description").
		Set("sa_parent_id = EXCLUDED.sa_parent_id").
		Set("type = EXCLUDED.type").
		Set("longitude = EXCLUDED.longitude").
		Set("latitude = EXCLUDED.latitude").
		Set("sa_level = EXCLUDED.sa_level").
		Set("code_type = EXCLUDED.code_type").
		Set("code = EXCLUDED.code").
		Set("external_id = EXCLUDED.external_id").
		Set("provider_id = EXCLUDED.provider_id").
		Set("sa_created_by = EXCLUDED.sa_created_by").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func applyClaim(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.Claim
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("name = EXCLUDED.name").
		Set("description = EXCLUDED.description").
		Set("sa_access_level = EXCLUDED.sa_access_level").
		Set("inherits = EXCLUDED.inherits").
		Set("type = EXCLUDED.type").
		Set("sa_created_by = EXCLUDED.sa_created_by").
		Set("sa_owner_id = EXCLUDED.sa_owner_id").
		Set("sa_root_id = EXCLUDED.sa_root_id").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func applyUserClaim(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.UserClaim
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("claim_id = EXCLUDED.claim_id").
		Set("user_id = EXCLUDED.user_id").
		Set("reason = EXCLUDED.reason").
		Set("starts_at = EXCLUDED.starts_at").
		Set("ends_at = EXCLUDED.ends_at").
		Set("sa_created_by = EXCLUDED.sa_created_by").
		Set("sa_owner_id = EXCLUDED.sa_owner_id").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func applyClaimObject(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.ClaimObject
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("reason = EXCLUDED.reason").
		Set("function = EXCLUDED.function").
		Set("claim_id = EXCLUDED.claim_id").
		Set("object_id = EXCLUDED.object_id").
		Set("object_type = EXCLUDED.object_type").
		Set("sa_level = EXCLUDED.sa_level").
		Set("inherits = EXCLUDED.inherits").
		Set("ref_id = EXCLUDED.ref_id").
		Set("sa_root_id = EXCLUDED.sa_root_id").
		Set("sa_created_by = EXCLUDED.sa_created_by").
		Set("sa_owner_id = EXCLUDED.sa_owner_id").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func applySamnaClient(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.SamnaClient
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("name = EXCLUDED.name").
		Set("description = EXCLUDED.description").
		Set("app_id = EXCLUDED.app_id").
		Set("client_id = EXCLUDED.client_id").
		Set("secret_hash = EXCLUDED.secret_hash").
		Set("status = EXCLUDED.status").
		Set("expires_at = EXCLUDED.expires_at").
		Set("sa_owner_id = EXCLUDED.sa_owner_id").
		Set("sa_created_by = EXCLUDED.sa_created_by").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func applySamnaSecret(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.SamnaSecret
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("app_id = EXCLUDED.app_id").
		Set("key = EXCLUDED.key").
		Set("value = EXCLUDED.value").
		Set("sa_owner_id = EXCLUDED.sa_owner_id").
		Set("sa_created_by = EXCLUDED.sa_created_by").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func applySamnaApp(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.SamnaApp
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("slug = EXCLUDED.slug").
		Set("name = EXCLUDED.name").
		Set("private_seed = EXCLUDED.private_seed").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func applyObjectField(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.ObjectField
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("app_id = EXCLUDED.app_id").
		Set("object_type = EXCLUDED.object_type").
		Set("name = EXCLUDED.name").
		Set("description = EXCLUDED.description").
		Set("json_schema = EXCLUDED.json_schema").
		Set("required = EXCLUDED.required").
		Set("value = EXCLUDED.value").
		Set("sa_owner_id = EXCLUDED.sa_owner_id").
		Set("sa_created_by = EXCLUDED.sa_created_by").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func applyUserField(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.UserField
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("field_id = EXCLUDED.field_id").
		Set("user_id = EXCLUDED.user_id").
		Set("value = EXCLUDED.value").
		Set("sa_owner_id = EXCLUDED.sa_owner_id").
		Set("sa_created_by = EXCLUDED.sa_created_by").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func applyUserRelation(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.UserRelation
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("user_id = EXCLUDED.user_id").
		Set("app_id = EXCLUDED.app_id").
		Set("description = EXCLUDED.description").
		Set("object_type = EXCLUDED.object_type").
		Set("object_id = EXCLUDED.object_id").
		Set("pinned = EXCLUDED.pinned").
		Set("priority = EXCLUDED.priority").
		Set("used_at = EXCLUDED.used_at").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func applyAudit(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.Audit
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("operation = EXCLUDED.operation").
		Set("message = EXCLUDED.message").
		Set("type = EXCLUDED.type").
		Set("object_type = EXCLUDED.object_type").
		Set("object_id = EXCLUDED.object_id").
		Set("sa_created_by = EXCLUDED.sa_created_by").
		Set("sa_owner_id = EXCLUDED.sa_owner_id").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func UpdateSeq(ctx context.Context, seq int64) error {
	count, err := db.Instance().NewSelect().
		Model((*db.DiscipleState)(nil)).
		Count(ctx)
	if err != nil {
		return err
	}

	if count == 0 {
		_, err = db.Instance().NewInsert().
			Model(&db.DiscipleState{
				LastAppliedSeq: seq,
				UpdatedAt:      time.Now(),
			}).
			Exec(ctx)
		return err
	}

	_, err = db.Instance().NewUpdate().
		Model((*db.DiscipleState)(nil)).
		Set("last_applied_seq = ?", seq).
		Set("sa_updated_at = ?", time.Now()).
		Where("TRUE").
		Exec(ctx)
	return err
}

func GetSeq(ctx context.Context) (int64, error) {
	var state db.DiscipleState
	err := db.Instance().NewSelect().
		Model(&state).
		Limit(1).
		Scan(ctx)
	if err != nil {
		return 0, nil
	}
	return state.LastAppliedSeq, nil
}

func WaitForSeq(ctx context.Context, targetSeq int64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		seq, err := GetSeq(ctx)
		if err != nil {
			return err
		}
		if seq >= targetSeq {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for seq %d", targetSeq)
}
