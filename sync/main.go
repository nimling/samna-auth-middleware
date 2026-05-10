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
	Seq       int64           `json:"seq"`
	Operation string          `json:"operation"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

type idEnvelope struct {
	ID json.RawMessage `json:"id"`
}

func ApplyPayload(ctx context.Context, payload SyncPayload) error {
	return db.Instance().RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, "SET LOCAL claimius.replay_mode = 'true'"); err != nil {
			return fmt.Errorf("set replay_mode: %w", err)
		}

		switch payload.Operation {
		case "upsert":
			return applyUpsert(ctx, tx, payload.EventType, payload.Payload)
		case "delete":
			return applyDelete(ctx, tx, payload.EventType, payload.Payload)
		default:
			return fmt.Errorf("unknown sync operation: %s", payload.Operation)
		}
	})
}

func applyUpsert(ctx context.Context, tx bun.Tx, eventType string, raw json.RawMessage) error {
	switch eventType {
	case "claimius.samna_app":
		return upsertSamnaApp(ctx, tx, raw)
	case "claimius.samna_user":
		return upsertSamnaUser(ctx, tx, raw)
	case "claimius.samna_client":
		return upsertSamnaClient(ctx, tx, raw)
	case "claimius.samna_secret":
		return upsertSamnaSecret(ctx, tx, raw)
	case "claimius.organization":
		return upsertOrganization(ctx, tx, raw)
	case "claimius.location":
		return upsertLocation(ctx, tx, raw)
	case "claimius.claim":
		return upsertClaim(ctx, tx, raw)
	case "claimius.user_claim":
		return upsertUserClaim(ctx, tx, raw)
	case "claimius.claim_object":
		return upsertClaimObject(ctx, tx, raw)
	case "claimius.object_field":
		return upsertObjectField(ctx, tx, raw)
	case "claimius.user_field":
		return upsertUserField(ctx, tx, raw)
	case "claimius.user_relation":
		return upsertUserRelation(ctx, tx, raw)
	case "claimius.audit":
		return upsertAudit(ctx, tx, raw)
	case "claimius.user_object":
		return upsertUserObject(ctx, tx, raw)
	case "claimius.object_users":
		return upsertObjectUsers(ctx, tx, raw)
	case "claimius.user_users":
		return upsertUserUsers(ctx, tx, raw)
	default:
		return fmt.Errorf("unknown event_type for upsert: %s", eventType)
	}
}

func applyDelete(ctx context.Context, tx bun.Tx, eventType string, raw json.RawMessage) error {
	var env idEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("unmarshal id envelope: %w", err)
	}
	switch eventType {
	case "claimius.user_object":
		return deleteByTextID(ctx, tx, (*db.UserObject)(nil), env.ID)
	case "claimius.object_users":
		return deleteByTextID(ctx, tx, (*db.ObjectUsers)(nil), env.ID)
	case "claimius.user_users":
		return deleteByTextID(ctx, tx, (*db.UserUsers)(nil), env.ID)
	default:
		return fmt.Errorf("delete not supported for event_type: %s", eventType)
	}
}

func deleteByTextID(ctx context.Context, tx bun.Tx, model any, id json.RawMessage) error {
	var key string
	if err := json.Unmarshal(id, &key); err != nil {
		return fmt.Errorf("unmarshal id as string: %w", err)
	}
	_, err := tx.NewDelete().Model(model).
		Where("sa_deleted_at IS NULL").Where("id = ?", key).Exec(ctx)
	return err
}

func upsertSamnaUser(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.SamnaUser
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("user_id = EXCLUDED.user_id").
		Set("app_id = EXCLUDED.app_id").
		Set("first_name = EXCLUDED.first_name").
		Set("last_name = EXCLUDED.last_name").
		Set("user_name = EXCLUDED.user_name").
		Set("user_image = EXCLUDED.user_image").
		Set("phone = EXCLUDED.phone").
		Set("email = EXCLUDED.email").
		Set("status = EXCLUDED.status").
		Set("type = EXCLUDED.type").
		Set("config = EXCLUDED.config").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func upsertOrganization(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.Organization
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("app_id = EXCLUDED.app_id").
		Set("name = EXCLUDED.name").
		Set("description = EXCLUDED.description").
		Set("type = EXCLUDED.type").
		Set("sa_owner_id = EXCLUDED.sa_owner_id").
		Set("sa_root_id = EXCLUDED.sa_root_id").
		Set("sa_location_id = EXCLUDED.sa_location_id").
		Set("sa_level = EXCLUDED.sa_level").
		Set("external_id = EXCLUDED.external_id").
		Set("provider_id = EXCLUDED.provider_id").
		Set("sa_created_by = EXCLUDED.sa_created_by").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func upsertLocation(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.Location
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("app_id = EXCLUDED.app_id").
		Set("sa_owner_id = EXCLUDED.sa_owner_id").
		Set("sa_parent_id = EXCLUDED.sa_parent_id").
		Set("sa_root_id = EXCLUDED.sa_root_id").
		Set("name = EXCLUDED.name").
		Set("description = EXCLUDED.description").
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

func upsertClaim(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.Claim
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("app_id = EXCLUDED.app_id").
		Set("name = EXCLUDED.name").
		Set("description = EXCLUDED.description").
		Set("sa_access = EXCLUDED.sa_access").
		Set("inherits = EXCLUDED.inherits").
		Set("type = EXCLUDED.type").
		Set("sa_owner_id = EXCLUDED.sa_owner_id").
		Set("sa_root_id = EXCLUDED.sa_root_id").
		Set("sa_created_by = EXCLUDED.sa_created_by").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func upsertUserClaim(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.UserClaim
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("app_id = EXCLUDED.app_id").
		Set("claim_id = EXCLUDED.claim_id").
		Set("user_id = EXCLUDED.user_id").
		Set("reason = EXCLUDED.reason").
		Set("starts_at = EXCLUDED.starts_at").
		Set("ends_at = EXCLUDED.ends_at").
		Set("sa_owner_id = EXCLUDED.sa_owner_id").
		Set("sa_created_by = EXCLUDED.sa_created_by").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func upsertClaimObject(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.ClaimObject
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("app_id = EXCLUDED.app_id").
		Set("claim_id = EXCLUDED.claim_id").
		Set("object_id = EXCLUDED.object_id").
		Set("object_type = EXCLUDED.object_type").
		Set("reason = EXCLUDED.reason").
		Set("scope = EXCLUDED.scope").
		Set("sa_access = EXCLUDED.sa_access").
		Set("inherits = EXCLUDED.inherits").
		Set("ref_id = EXCLUDED.ref_id").
		Set("sa_owner_id = EXCLUDED.sa_owner_id").
		Set("sa_root_id = EXCLUDED.sa_root_id").
		Set("sa_created_by = EXCLUDED.sa_created_by").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func upsertSamnaClient(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
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

func upsertSamnaSecret(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.SamnaSecret
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("app_id = EXCLUDED.app_id").
		Set("key = EXCLUDED.key").
		Set("value = EXCLUDED.value").
		Set("expires_at = EXCLUDED.expires_at").
		Set("sa_owner_id = EXCLUDED.sa_owner_id").
		Set("sa_created_by = EXCLUDED.sa_created_by").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func upsertSamnaApp(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.SamnaApp
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("slug = EXCLUDED.slug").
		Set("name = EXCLUDED.name").
		Set("description = EXCLUDED.description").
		Set("contact_email = EXCLUDED.contact_email").
		Set("app_image = EXCLUDED.app_image").
		Set("redirect_uri = EXCLUDED.redirect_uri").
		Set("sync_uri = EXCLUDED.sync_uri").
		Set("provider_ids = EXCLUDED.provider_ids").
		Set("status = EXCLUDED.status").
		Set("private_key = EXCLUDED.private_key").
		Set("private_seed = EXCLUDED.private_seed").
		Set("app_secret = EXCLUDED.app_secret").
		Set("secret_version = EXCLUDED.secret_version").
		Set("claim_id = EXCLUDED.claim_id").
		Set("style = EXCLUDED.style").
		Set("sa_owner_id = EXCLUDED.sa_owner_id").
		Set("sa_created_by = EXCLUDED.sa_created_by").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func upsertObjectField(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
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

func upsertUserField(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.UserField
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("app_id = EXCLUDED.app_id").
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

func upsertUserRelation(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.UserRelation
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("app_id = EXCLUDED.app_id").
		Set("user_id = EXCLUDED.user_id").
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

func upsertAudit(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.Audit
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("app_id = EXCLUDED.app_id").
		Set("operation = EXCLUDED.operation").
		Set("message = EXCLUDED.message").
		Set("type = EXCLUDED.type").
		Set("object_type = EXCLUDED.object_type").
		Set("object_id = EXCLUDED.object_id").
		Set("sa_owner_id = EXCLUDED.sa_owner_id").
		Set("sa_created_by = EXCLUDED.sa_created_by").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func upsertUserObject(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.UserObject
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("app_id = EXCLUDED.app_id").
		Set("user_id = EXCLUDED.user_id").
		Set("object_id = EXCLUDED.object_id").
		Set("object_type = EXCLUDED.object_type").
		Set("sa_access = EXCLUDED.sa_access").
		Set("scope = EXCLUDED.scope").
		Set("grants = EXCLUDED.grants").
		Set("direct_grant = EXCLUDED.direct_grant").
		Set("sa_owner_id = EXCLUDED.sa_owner_id").
		Set("sa_location_id = EXCLUDED.sa_location_id").
		Set("sa_root_id = EXCLUDED.sa_root_id").
		Set("sa_name = EXCLUDED.sa_name").
		Set("sa_description = EXCLUDED.sa_description").
		Set("sa_link = EXCLUDED.sa_link").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func upsertObjectUsers(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.ObjectUsers
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("app_id = EXCLUDED.app_id").
		Set("object_id = EXCLUDED.object_id").
		Set("object_type = EXCLUDED.object_type").
		Set("user_id = EXCLUDED.user_id").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Exec(ctx)
	return err
}

func upsertUserUsers(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.UserUsers
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	_, err := tx.NewInsert().Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("app_id = EXCLUDED.app_id").
		Set("viewer_id = EXCLUDED.viewer_id").
		Set("target_user_id = EXCLUDED.target_user_id").
		Set("first_shared_at = EXCLUDED.first_shared_at").
		Set("last_shared_at = EXCLUDED.last_shared_at").
		Set("sharing_object_count = EXCLUDED.sharing_object_count").
		Exec(ctx)
	return err
}

func UpdateSeq(ctx context.Context, seq int64) error {
	count, err := db.Instance().NewSelect().ExcludeColumn("app_id").
		Model((*db.DiscipleState)(nil)).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
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
		Where("sa_deleted_at IS NULL").
		Set("last_applied_seq = ?", seq).
		Set("sa_updated_at = ?", time.Now()).
		Where("TRUE").
		Exec(ctx)
	return err
}

func GetSeq(ctx context.Context) (int64, error) {
	var state db.DiscipleState
	err := db.Instance().NewSelect().ExcludeColumn("app_id").
		Model(&state).
		Where("sa_deleted_at IS NULL").
		ExcludeColumn("sa_deleted_at").
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
