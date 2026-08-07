package sam

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	sdb "github.com/nimling/samna-auth-middleware/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/internal/store"
	"github.com/nimling/samna-auth-middleware/sauth"
	"github.com/nimling/samna-auth-middleware/shared"
	"github.com/uptrace/bun"
)

const (
	activeInterval             = 100 * time.Millisecond
	maxIdleInterval            = 15 * time.Second
	maxBackoff                 = 30 * time.Second
	emptyPullsBeforeReconcile  = 40
	failedPullsBeforeReconcile = 10
)

// reportStalledCursor records that the disciple has stopped making progress at
// its cursor. Two patterns produce this and neither recovers on its own: a
// cursor above the prophet's highest sequence, which yields empty pulls
// forever, and an event the replica cannot apply, which fails forever. The loop
// answers both with a snapshot and leaves an audit trail of the reconcile.
func reportStalledCursor(ctx context.Context, log echo.Logger, reason string) {
	seq, err := GetSeq(ctx)
	if err != nil {
		log.Errorf("disciple sync: read cursor before reconcile: %v", err)
		return
	}
	log.Warnf("disciple sync: cursor %d %s, reconciling from snapshot", seq, reason)
	sdb.Write(
		"disciple sync reconcile",
		"update",
		"claimius.disciple_state",
		uuid.Nil,
		uuid.Nil,
		nil,
		fmt.Sprintf("cursor %d %s, pulling a fresh snapshot", seq, reason),
	)
}

// SyncStart launches the background pull loop that keeps the disciple database
// current. Call it once after the server starts. It runs until the context ends
// and logs failures through the database logger.
func SyncStart(ctx context.Context) {
	if shared.GetDebugMode() != shared.DebugModeNone {
		return
	}
	go func() {
		log := sdb.Instance().Logger
		report := func(err error) {
			if err != nil {
				log.Errorf("disciple sync: %v", err)
			}
		}
		if seq, err := GetSeq(ctx); err != nil {
			report(fmt.Errorf("get current seq: %w", err))
		} else if seq == 0 {
			report(PullSnapshot(ctx))
		}
		backoff := time.Second
		idle := time.Duration(0)
		empty := 0
		failed := 0
		for {
			wait := activeInterval
			applied, err := PullSync(ctx)
			switch {
			case err != nil:
				report(err)
				wait = backoff
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				failed++
				if failed >= failedPullsBeforeReconcile {
					failed = 0
					backoff = time.Second
					reportStalledCursor(ctx, log, fmt.Sprintf("failed to apply %d consecutive pulls, last error %v", failedPullsBeforeReconcile, err))
					report(PullSnapshot(ctx))
				}
			case applied > 0:
				backoff = time.Second
				idle = 0
				empty = 0
				failed = 0
			default:
				backoff = time.Second
				empty++
				failed = 0
				if idle == 0 {
					idle = activeInterval
				}
				idle *= 2
				if idle > maxIdleInterval {
					idle = maxIdleInterval
				}
				wait = idle
				if empty >= emptyPullsBeforeReconcile {
					empty = 0
					idle = 0
					reportStalledCursor(ctx, log, fmt.Sprintf("returned no events %d consecutive pulls", emptyPullsBeforeReconcile))
					report(PullSnapshot(ctx))
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
	}()
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
	return sdb.Instance().DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, "SET LOCAL claimius.replay_mode = 'true'"); err != nil {
			return fmt.Errorf("set replay_mode: %w", err)
		}

		switch payload.Operation {
		case "upsert":
			if err := applyUpsert(ctx, tx, payload.EventType, payload.Payload); err != nil {
				return err
			}
		case "delete":
			if err := applyDelete(ctx, tx, payload.EventType, payload.Payload); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown sync operation: %s", payload.Operation)
		}

		return recomputeExternal(ctx, tx, payload.EventType, payload.Payload)
	})
}

type claimBindingEnvelope struct {
	ID      *uuid.UUID `json:"id"`
	ClaimID *uuid.UUID `json:"claim_id"`
	UserID  *uuid.UUID `json:"user_id"`
}

func recomputeExternal(ctx context.Context, tx bun.Tx, eventType string, raw json.RawMessage) error {
	var envelope claimBindingEnvelope
	switch eventType {
	case "claimius.claim":
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return fmt.Errorf("unmarshal claim envelope: %w", err)
		}
		envelope.ClaimID = envelope.ID
	case "claimius.claim_object", "claimius.user_claim":
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return fmt.Errorf("unmarshal claim binding envelope: %w", err)
		}
	default:
		return nil
	}

	userIDs := make([]uuid.UUID, 0, 1)
	if envelope.UserID != nil {
		userIDs = append(userIDs, *envelope.UserID)
	} else if envelope.ClaimID != nil {
		if err := tx.NewSelect().
			Model((*shared.UserClaim)(nil)).
			Column("user_id").
			Where("claim_id = ?", *envelope.ClaimID).
			Where("sa_deleted_at IS NULL").
			Scan(ctx, &userIDs); err != nil {
			return fmt.Errorf("select claim holders: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, "SET LOCAL claimius.replay_mode = 'false'"); err != nil {
		return fmt.Errorf("clear replay_mode: %w", err)
	}
	defer func() {
		_, _ = tx.ExecContext(ctx, "SET LOCAL claimius.replay_mode = 'true'")
	}()

	for _, userID := range userIDs {
		if _, err := tx.ExecContext(ctx, "SELECT claimius.recompute_external_access(?)", userID); err != nil {
			return fmt.Errorf("recompute external access for %s: %w", userID, err)
		}
	}
	return nil
}

func applyUpsert(ctx context.Context, tx bun.Tx, eventType string, raw json.RawMessage) error {
	switch eventType {
	case "claimius.samna_user":
		var incoming shared.SamnaUser
		if err := sauth.Unmarshal(raw, &incoming); err != nil {
			return fmt.Errorf("unmarshal: %w", err)
		}
		if _, err := tx.NewUpdate().
			Model((*shared.SamnaUser)(nil)).
			Set("sa_deleted_at = now()").
			Set("sa_updated_at = now()").
			Where("user_id = ?", incoming.UserID).
			Where("id <> ?", incoming.ID).
			Where("sa_deleted_at IS NULL").
			Exec(ctx); err != nil {
			return fmt.Errorf("retire superseded samna_user %s: %w", incoming.UserID, err)
		}
		return store.Upsert(ctx, tx, &incoming)
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
	case "claimius.samna_user":
		return deleteEntity[shared.SamnaUser](ctx, tx, env.ID)
	case "claimius.samna_client":
		return deleteEntity[shared.SamnaClient](ctx, tx, env.ID)
	case "claimius.samna_secret":
		return deleteEntity[shared.SamnaSecret](ctx, tx, env.ID)
	case "claimius.organization":
		return deleteEntity[shared.Organization](ctx, tx, env.ID)
	case "claimius.location":
		return deleteEntity[shared.Location](ctx, tx, env.ID)
	case "claimius.claim":
		return deleteEntity[shared.Claim](ctx, tx, env.ID)
	case "claimius.user_claim":
		return deleteEntity[shared.UserClaim](ctx, tx, env.ID)
	case "claimius.claim_object":
		return deleteEntity[shared.ClaimObject](ctx, tx, env.ID)
	case "claimius.object_field":
		return deleteEntity[shared.ObjectField](ctx, tx, env.ID)
	case "claimius.user_field":
		return deleteEntity[shared.UserField](ctx, tx, env.ID)
	case "claimius.user_relation":
		return deleteEntity[shared.UserRelation](ctx, tx, env.ID)
	case "claimius.audit":
		return deleteEntity[shared.Audit](ctx, tx, env.ID)
	case "claimius.user_object":
		return deleteEntity[shared.UserObject](ctx, tx, env.ID)
	case "claimius.object_users":
		return deleteEntity[shared.ObjectUsers](ctx, tx, env.ID)
	case "claimius.user_users":
		return deleteEntity[shared.UserUsers](ctx, tx, env.ID)
	default:
		return fmt.Errorf("delete not supported for event_type: %s", eventType)
	}
}

func deleteEntity[T any](ctx context.Context, tx bun.Tx, id json.RawMessage) error {
	var key string
	if err := json.Unmarshal(id, &key); err != nil {
		return fmt.Errorf("unmarshal id: %w", err)
	}
	entity := new(T)
	result, err := tx.NewDelete().Model(entity).
		Where("id = ?", key).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete %T: %w", entity, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete %T rows: %w", entity, err)
	}
	if rows == 0 {
		sdb.Instance().Logger.Debugf("disciple sync: delete %T id %s matched no row", entity, key)
	}
	return nil
}

func UpdateSeq(ctx context.Context, seq int64) error {
	return sdb.Instance().DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
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
	err := sdb.Instance().DB.NewSelect().
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
