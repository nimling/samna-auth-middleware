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
			return applyEntity[db.Organization](ctx, tx, payload.Obj)
		case "location":
			return applyEntity[db.Location](ctx, tx, payload.Obj)
		case "samna_user":
			return applyEntity[db.SamnaUser](ctx, tx, payload.Obj)
		case "claim":
			return applyEntity[db.Claim](ctx, tx, payload.Obj)
		case "user_claim":
			return applyEntity[db.UserClaim](ctx, tx, payload.Obj)
		case "claim_object":
			return applyEntity[db.ClaimObject](ctx, tx, payload.Obj)
		case "samna_client":
			return applyEntity[db.SamnaClient](ctx, tx, payload.Obj)
		case "samna_secret":
			return applySecret(ctx, tx, payload.Obj)
		case "samna_app":
			return applyApp(ctx, tx, payload.Obj)
		case "object_field":
			return applyEntity[db.ObjectField](ctx, tx, payload.Obj)
		case "user_field":
			return applyEntity[db.UserField](ctx, tx, payload.Obj)
		case "user_relation":
			return applyEntity[db.UserRelation](ctx, tx, payload.Obj)
		case "audit":
			return applyAudit(ctx, tx, payload.Obj)
		default:
			return fmt.Errorf("unknown sync type: %s", payload.Type)
		}
	})
}

func applyEntity[T any](ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity T
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal entity: %w", err)
	}

	_, err := tx.NewInsert().
		Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Exec(ctx)

	return err
}

func applySecret(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.SamnaSecret
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal entity: %w", err)
	}

	_, err := tx.NewInsert().
		Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("key = EXCLUDED.key").
		Set("value_encrypted = EXCLUDED.value_encrypted").
		Set("sa_owner_id = EXCLUDED.sa_owner_id").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Exec(ctx)

	return err
}

func applyApp(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.SamnaApp
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal entity: %w", err)
	}

	_, err := tx.NewInsert().
		Model(&entity).
		On("CONFLICT (id) DO UPDATE").
		Set("slug = EXCLUDED.slug").
		Set("name = EXCLUDED.name").
		Set("private_seed = EXCLUDED.private_seed").
		Set("sa_updated_at = EXCLUDED.sa_updated_at").
		Set("sa_deleted_at = EXCLUDED.sa_deleted_at").
		Exec(ctx)

	return err
}

func applyAudit(ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity db.Audit
	if err := json.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal entity: %w", err)
	}

	_, err := tx.NewInsert().
		Model(&entity).
		On("CONFLICT (id) DO UPDATE").
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
