package sam

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	sdb "github.com/nimling/samna-auth-middleware/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/internal/store"
	"github.com/nimling/samna-auth-middleware/sauth"
	"github.com/nimling/samna-auth-middleware/shared"
	"github.com/nimling/samna-auth-middleware/utils"
	"github.com/uptrace/bun"
)

const (
	activeInterval             = 100 * time.Millisecond
	maxBackoff                 = 30 * time.Second
	failedPullsBeforeReconcile = 10
	leadInterval               = 5 * time.Second
	syncHold                   = 25 * time.Second
	syncHoldMargin             = 10 * time.Second
)

// reportStalledCursor records that the disciple has stopped making progress at
// its cursor. Two patterns produce this and neither recovers on its own: a
// cursor the prophet's feed cannot serve, which the prophet answers with 409,
// and an event the replica cannot apply, which fails forever. The loop answers
// both with a snapshot and leaves an audit trail of the reconcile.
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
// current. Call it once after the server starts, on every pod. Only the pod
// holding the disciple's sync lock pulls; the others wait to take over. It runs
// until the context ends and logs failures through the database logger.
func SyncStart(ctx context.Context) {
	if shared.GetDebugMode() != shared.DebugModeNone {
		return
	}
	go LeadSync(ctx, sdb.Instance().DB, SyncLockKey(utils.Cfg().AppSlug), leadInterval, pullLoop)
}

// SyncLockKey names the advisory lock that makes one process the sync consumer
// of the disciple database of an app.
func SyncLockKey(appSlug string) string {
	return "claimius.disciple_sync:" + appSlug
}

// LeadSync runs work for as long as this process holds the session level
// advisory lock on key, on a dedicated connection taken from db. A process that
// does not get the lock does no sync work and retries every interval, so N pods
// apply each event once instead of N times. The lock lives and dies with its
// connection: when the holder goes away the server releases it and another
// process takes over, and a holder whose connection stops answering cancels
// its work and discards the connection before it tries again.
func LeadSync(ctx context.Context, db *shared.DB, key string, interval time.Duration, work func(context.Context)) {
	for {
		conn, err := takeSyncLock(ctx, db, key)
		if err != nil && ctx.Err() == nil {
			db.Logger.Errorf("disciple sync: take sync lock: %v", err)
		}
		if conn != nil {
			holdSyncLock(ctx, conn, interval, work)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func takeSyncLock(ctx context.Context, db *shared.DB, key string) (*bun.Conn, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var held bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock(hashtextextended(?, 0))", key).Scan(&held); err != nil {
		discardConn(&conn)
		return nil, err
	}
	if !held {
		discardConn(&conn)
		return nil, nil
	}
	return &conn, nil
}

func holdSyncLock(ctx context.Context, conn *bun.Conn, interval time.Duration, work func(context.Context)) {
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		work(workCtx)
	}()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			discardConn(conn)
			return
		case <-ticker.C:
			checkCtx, stop := context.WithTimeout(ctx, interval)
			_, err := conn.ExecContext(checkCtx, "SELECT 1")
			stop()
			if err != nil {
				cancel()
				<-done
				discardConn(conn)
				return
			}
		}
	}
}

// discardConn closes the session behind a connection instead of returning it
// to the pool, so a session level lock never outlives the process that took it.
func discardConn(conn *bun.Conn) {
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
}

func pullLoop(ctx context.Context) {
	log := sdb.Instance().Logger
	report := func(err error) {
		if err != nil && ctx.Err() == nil {
			log.Errorf("disciple sync: %v", err)
		}
	}
	if seq, err := GetSeq(ctx); err != nil {
		report(fmt.Errorf("get current seq: %w", err))
	} else if seq == 0 {
		report(PullSnapshot(ctx))
	}
	backoff := time.Second
	failed := 0
	for {
		wait := activeInterval
		_, err := PullSync(ctx)
		switch {
		case errors.Is(err, ErrSnapshotRequired):
			backoff = time.Second
			failed = 0
			reportStalledCursor(ctx, log, "was refused by the prophet feed")
			report(PullSnapshot(ctx))
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
		default:
			backoff = time.Second
			failed = 0
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
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

// ApplyPage applies one page of payloads and moves the cursor from from to to
// in one transaction under replay_mode, so a page of p events costs one commit
// and a page that fails anywhere rolls back whole, cursor included, and is
// pulled again. The commit does not wait for its WAL flush: a page lost to a
// crash is a cursor that did not move, and the page is pulled again. The
// cursor moves first and only from the position the page was pulled at, which
// holds its row for the page and refuses a page another consumer already
// applied.
func ApplyPage(ctx context.Context, from, to int64, payloads []SyncPayload) error {
	return sdb.Instance().DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, "SET LOCAL claimius.replay_mode = 'true'"); err != nil {
			return fmt.Errorf("set replay_mode: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "SET LOCAL synchronous_commit = off"); err != nil {
			return fmt.Errorf("set synchronous_commit: %w", err)
		}
		if err := UpdateSeq(ctx, tx, from, to); err != nil {
			return err
		}
		for _, payload := range payloads {
			if err := ApplyPayload(ctx, tx, payload); err != nil {
				return fmt.Errorf("apply %s seq %d: %w", payload.EventType, payload.Seq, err)
			}
		}
		return nil
	})
}

func ApplyPayload(ctx context.Context, tx bun.Tx, payload SyncPayload) error {
	if payload.EventType == "claimius.samna_app" {
		return nil
	}
	payload.Payload = stripAppID(payload.Payload)

	switch payload.Operation {
	case "upsert":
		changed, err := applyUpsert(ctx, tx, payload.EventType, payload.Payload)
		if err != nil || !changed {
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
}

type claimBindingEnvelope struct {
	ID         *uuid.UUID `json:"id"`
	ClaimID    *uuid.UUID `json:"claim_id"`
	UserID     *uuid.UUID `json:"user_id"`
	ObjectType *string    `json:"object_type"`
	ObjectID   *uuid.UUID `json:"object_id"`
}

// ClaimAreas resolves the claim areas a synced payload moves. A claim payload
// names itself, a binding names the claim it binds, and a claim_object linking
// one claim to another moves both. Any other event type moves no claim area.
func ClaimAreas(eventType string, raw json.RawMessage) ([]uuid.UUID, error) {
	var envelope claimBindingEnvelope
	switch eventType {
	case "claimius.claim":
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, fmt.Errorf("unmarshal claim envelope: %w", err)
		}
		envelope.ClaimID = envelope.ID
	case "claimius.claim_object", "claimius.user_claim":
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, fmt.Errorf("unmarshal claim binding envelope: %w", err)
		}
	default:
		return nil, nil
	}

	areas := make([]uuid.UUID, 0, 2)
	if envelope.ClaimID != nil {
		areas = append(areas, *envelope.ClaimID)
	}
	if eventType == "claimius.claim_object" &&
		envelope.ObjectType != nil && *envelope.ObjectType == "claimius.claim" &&
		envelope.ObjectID != nil {
		areas = append(areas, *envelope.ObjectID)
	}
	return areas, nil
}

// recomputeExternal marks the claim areas a payload moved as outstanding. The
// disciple carries external tables the prophet knows nothing about, so their
// access settles locally, and the replay guard on the claim trigger means a
// synced payload raises nothing by itself. Marking an area costs one row
// whatever the claim reaches, and reconcile_if_pending settles it on the next
// read that targets it. An assignment moves the area of its one user only.
func recomputeExternal(ctx context.Context, tx bun.Tx, eventType string, raw json.RawMessage) error {
	areas, err := ClaimAreas(eventType, raw)
	if err != nil {
		return err
	}
	var assignee *uuid.UUID
	if eventType == "claimius.user_claim" {
		var envelope claimBindingEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return fmt.Errorf("unmarshal assignment envelope: %w", err)
		}
		assignee = envelope.UserID
	}
	for _, claimID := range areas {
		if _, err := tx.ExecContext(ctx, "SELECT claimius._raise_claim_diff(?, ?)", claimID, assignee); err != nil {
			return fmt.Errorf("raise claim diff for %s: %w", claimID, err)
		}
	}
	return nil
}

func applyUpsert(ctx context.Context, tx bun.Tx, eventType string, raw json.RawMessage) (bool, error) {
	switch eventType {
	case "claimius.samna_user":
		var incoming shared.SamnaUser
		if err := sauth.Unmarshal(raw, &incoming); err != nil {
			return false, fmt.Errorf("unmarshal: %w", err)
		}
		if _, err := tx.NewUpdate().
			Model((*shared.SamnaUser)(nil)).
			Set("sa_deleted_at = now()").
			Set("sa_updated_at = now()").
			Where("user_id = ?", incoming.UserID).
			Where("id <> ?", incoming.ID).
			Where("sa_deleted_at IS NULL").
			Exec(ctx); err != nil {
			return false, fmt.Errorf("retire superseded samna_user %s: %w", incoming.UserID, err)
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
	default:
		return false, fmt.Errorf("unknown event_type for upsert: %s", eventType)
	}
}

func upsertEntity[T any](ctx context.Context, tx bun.Tx, raw json.RawMessage) (bool, error) {
	var entity T
	if err := sauth.Unmarshal(raw, &entity); err != nil {
		return false, fmt.Errorf("unmarshal: %w", err)
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

// UpdateSeq moves the cursor from from to to. A page moves it forward; a
// snapshot sets it to the snapshot's sequence, below a cursor the prophet no
// longer serves when that is what sent the disciple to the snapshot.
func UpdateSeq(ctx context.Context, tx bun.Tx, from, to int64) error {
	result, err := tx.NewUpdate().
		Model((*shared.DiscipleState)(nil)).
		Set("last_applied_seq = ?", to).
		Set("sa_updated_at = ?", time.Now()).
		Where("last_applied_seq = ?", from).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("update seq: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update seq rows: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("update seq: cursor moved from %d before the page applied", from)
	}
	return nil
}

// GetState reads the disciple's cursor row. Its id names the disciple database
// to the prophet as a sync consumer. A disciple with no row yet reads as a zero
// state.
func GetState(ctx context.Context) (shared.DiscipleState, error) {
	var state shared.DiscipleState
	err := sdb.Instance().DB.NewSelect().
		Model(&state).
		Limit(1).
		Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return shared.DiscipleState{}, nil
	}
	return state, err
}

func GetSeq(ctx context.Context) (int64, error) {
	state, err := GetState(ctx)
	if err != nil {
		return 0, err
	}
	return state.LastAppliedSeq, nil
}

// WaitForSeq waits until the disciple's cursor reaches targetSeq, the sequence
// a routed write answered with, so a read that follows sees that write. It
// gives up after timeout or when ctx ends.
func WaitForSeq(ctx context.Context, targetSeq int64, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		seq, err := GetSeq(ctx)
		if err != nil && ctx.Err() == nil {
			return err
		}
		if err == nil && seq >= targetSeq {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for seq %d: %w", targetSeq, ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}
