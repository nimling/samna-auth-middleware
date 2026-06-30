package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	db "github.com/nimling/samna-auth-middleware/disciple/db"
	"github.com/nimling/samna-auth-middleware/internal/store"
	"github.com/nimling/samna-auth-middleware/sauth"
	"github.com/nimling/samna-auth-middleware/shared"
	"github.com/uptrace/bun"
)

func Start(ctx context.Context) <-chan error {
	errs := make(chan error, 1)
	if shared.GetDebugMode() != shared.DebugModeNone {
		return errs
	}
	go func() {
		report := func(err error) {
			if err == nil {
				return
			}
			select {
			case errs <- err:
			default:
			}
		}
		if seq, err := GetSeq(ctx); err != nil {
			report(fmt.Errorf("get current seq: %w", err))
		} else if seq == 0 {
			report(PullSnapshot(ctx))
		}
		backoff := time.Second
		const maxBackoff = 30 * time.Second
		for {
			wait := 100 * time.Millisecond
			if err := PullSync(ctx); err != nil {
				report(err)
				wait = backoff
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			} else {
				backoff = time.Second
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
	}()
	return errs
}

type SyncPayload struct {
	Seq       int64           `json:"seq"`
	Operation string          `json:"operation"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

type idEnvelope struct {
	ID json.RawMessage `json:"id"`
}

func stripAppID(raw json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw
	}
	if _, ok := m["app_id"]; !ok {
		return raw
	}
	delete(m, "app_id")
	cleaned, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return cleaned
}

func ApplyPayload(ctx context.Context, payload SyncPayload) error {
	if payload.EventType == "claimius.samna_app" {
		return nil
	}
	payload.Payload = stripAppID(payload.Payload)
	return db.Instance().DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
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
	case "claimius.samna_user":
		return upsertEntity[shared.SamnaUser](ctx, tx, raw)
	case "claimius.samna_client":
		return upsertEntity[shared.SamnaClient](ctx, tx, raw)
	case "claimius.samna_secret":
		return upsertEntity[shared.SamnaSecret](ctx, tx, raw)
	case "claimius.organization":
		return upsertEntity[shared.Organization](ctx, tx, raw)
	case "claimius.location":
		return upsertEntity[shared.Location](ctx, tx, raw)
	case "claimius.claim":
		return upsertEntity[shared.Claim](ctx, tx, raw)
	case "claimius.user_claim":
		return upsertEntity[shared.UserClaim](ctx, tx, raw)
	case "claimius.claim_object":
		return upsertEntity[shared.ClaimObject](ctx, tx, raw)
	case "claimius.object_field":
		return upsertEntity[shared.ObjectField](ctx, tx, raw)
	case "claimius.user_field":
		return upsertEntity[shared.UserField](ctx, tx, raw)
	case "claimius.user_relation":
		return upsertEntity[shared.UserRelation](ctx, tx, raw)
	case "claimius.audit":
		return upsertEntity[shared.Audit](ctx, tx, raw)
	case "claimius.user_object":
		return upsertEntity[shared.UserObject](ctx, tx, raw)
	case "claimius.object_users":
		return upsertEntity[shared.ObjectUsers](ctx, tx, raw)
	case "claimius.user_users":
		return upsertEntity[shared.UserUsers](ctx, tx, raw)
	default:
		return fmt.Errorf("unknown event_type for upsert: %s", eventType)
	}
}

func upsertEntity[T any](ctx context.Context, tx bun.Tx, raw json.RawMessage) error {
	var entity T
	if err := sauth.Unmarshal(raw, &entity); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	return store.Upsert(ctx, tx, &entity)
}

func applyDelete(ctx context.Context, tx bun.Tx, eventType string, raw json.RawMessage) error {
	var env idEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("unmarshal id envelope: %w", err)
	}
	switch eventType {
	case "claimius.user_object":
		return deleteByTextID(ctx, tx, (*shared.UserObject)(nil), env.ID)
	case "claimius.object_users":
		return deleteByTextID(ctx, tx, (*shared.ObjectUsers)(nil), env.ID)
	case "claimius.user_users":
		return deleteByTextID(ctx, tx, (*shared.UserUsers)(nil), env.ID)
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
		Where("id = ?", key).Exec(ctx)
	return err
}

func UpdateSeq(ctx context.Context, seq int64) error {
	return db.Instance().DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var state shared.DiscipleState
		if err := tx.NewSelect().
			Model(&state).
			Limit(1).
			Scan(ctx); err != nil {
			return err
		}
		_, err := tx.NewUpdate().
			Model(&state).
			Set("last_applied_seq = ?", seq).
			Set("sa_updated_at = ?", time.Now()).
			Where("id = ?", state.ID).
			Where("last_applied_seq < ?", seq).
			Exec(ctx)
		return err
	})
}

func GetSeq(ctx context.Context) (int64, error) {
	var state shared.DiscipleState
	err := db.Instance().DB.NewSelect().
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
