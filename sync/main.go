package sync

import (
	"context"
	"encoding/json"
	"fmt"
	stdsync "sync"
	"time"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/shared"
	"github.com/nimling/samna-auth-middleware/internal/store"
	"github.com/nimling/samna-auth-middleware/sauth"
	"github.com/uptrace/bun"
)

var (
	discipleAppMu stdsync.Mutex
	discipleAppID uuid.UUID
)

func resolveDiscipleApp(ctx context.Context) uuid.UUID {
	discipleAppMu.Lock()
	defer discipleAppMu.Unlock()
	if discipleAppID != uuid.Nil {
		return discipleAppID
	}
	var id uuid.UUID
	if err := shared.Instance().NewRaw("SELECT claimius.get_disciple_app_id()").Scan(ctx, &id); err == nil {
		discipleAppID = id
	}
	return discipleAppID
}

func payloadScope(eventType string, raw json.RawMessage) (uuid.UUID, bool) {
	var m struct {
		AppID *uuid.UUID `json:"app_id"`
		ID    *uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return uuid.Nil, false
	}
	if m.AppID != nil {
		return *m.AppID, true
	}
	if eventType == "claimius.samna_app" && m.ID != nil {
		return *m.ID, true
	}
	return uuid.Nil, false
}

func Bootstrap(ctx context.Context) error {
	seq, err := GetSeq(ctx)
	if err != nil {
		return fmt.Errorf("get current seq: %w", err)
	}
	if seq == 0 {
		return PullSnapshot(ctx)
	}
	return PullSync(ctx)
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

func ApplyPayload(ctx context.Context, payload SyncPayload) error {
	if appID := resolveDiscipleApp(ctx); appID != uuid.Nil {
		if scope, ok := payloadScope(payload.EventType, payload.Payload); ok && scope != appID {
			return nil
		}
	}
	return shared.Instance().RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
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
		return upsertEntity[shared.SamnaApp](ctx, tx, raw)
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
		Where("sa_deleted_at IS NULL").Where("id = ?", key).Exec(ctx)
	return err
}

func UpdateSeq(ctx context.Context, seq int64) error {
	return shared.Instance().RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
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
	err := shared.Instance().NewSelect().
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
