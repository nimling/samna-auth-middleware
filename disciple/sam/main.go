package sam

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nimling/samna-auth-middleware/v2/client"
	sdb "github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/v2/internal/store"
	"github.com/nimling/samna-auth-middleware/v2/sauth"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/utils"
	"github.com/uptrace/bun"
)

const (
	activeInterval             = 100 * time.Millisecond
	maxBackoff                 = 30 * time.Second
	failedPullsBeforeReconcile = 10
	leadInterval               = 5 * time.Second
	syncHold                   = 25 * time.Second
	syncHoldMargin             = 10 * time.Second
	drainInterval              = time.Second
	drainPairs                 = 1000
	moveInterval               = time.Minute
)

type Replica struct {
	DB   *shared.DB
	Peer *shared.ProphetPeer
}

func Disciple() *Replica {
	return &Replica{DB: sdb.Instance().DB}
}

type State struct {
	ID       uuid.UUID
	Cursor   client.SyncCursor
	Prophet  string
	Endpoint string
}

func (r *Replica) reportStalledCursor(ctx context.Context, reason string) string {
	state, err := r.GetState(ctx)
	if err != nil {
		r.DB.Logger.Errorf("sync: read cursor before reconcile: %v", err)
		return fmt.Sprintf("cursor %s, reconciled from a fresh snapshot", reason)
	}
	r.DB.Logger.Warnf("sync: cursor %s %s, reconciling from snapshot", state.Cursor, reason)
	return fmt.Sprintf("cursor %s %s, reconciled from a fresh snapshot", state.Cursor, reason)
}

func SyncStart(ctx context.Context) {
	if shared.GetDebugMode() != shared.DebugModeNone {
		return
	}
	go bindClient(ctx, Disciple(), leadInterval)
	go shared.LeadSync(ctx, sdb.Instance().DB, SyncLockKey(utils.Cfg().AppSlug), leadInterval, Disciple().Pull)
}

func PeerSyncStart(ctx context.Context, db *shared.DB) {
	if shared.GetDebugMode() != shared.DebugModeNone {
		return
	}
	go func() {
		started := make(map[uuid.UUID]bool)
		for {
			var peers []shared.ProphetPeer
			if err := db.NewSelect().Model(&peers).Scan(ctx); err != nil && ctx.Err() == nil {
				db.Logger.Errorf("peer sync: read prophet_peer: %v", err)
			}
			for i := range peers {
				if started[peers[i].ID] {
					continue
				}
				started[peers[i].ID] = true
				r := &Replica{DB: db, Peer: &peers[i]}
				go shared.LeadSync(ctx, db, "claimius.prophet_peer:"+peers[i].ID.String(), leadInterval, r.Pull)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(leadInterval):
			}
		}
	}()
}

func bindClient(ctx context.Context, r *Replica, interval time.Duration) {
	for {
		if state, err := r.GetState(ctx); err == nil {
			client.Bind(state.Endpoint, state.Prophet)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func SyncLockKey(appSlug string) string {
	return "claimius.disciple_sync:" + appSlug
}

func (r *Replica) Pull(ctx context.Context) {
	log := r.DB.Logger
	report := func(err error) {
		if err != nil && ctx.Err() == nil {
			log.Errorf("sync: %v", err)
		}
	}
	if state, err := r.GetState(ctx); err != nil {
		report(fmt.Errorf("get current seq: %w", err))
	} else if state.Cursor.IsZero() {
		report(r.PullSnapshot(ctx, "", ""))
	}
	backoff := time.Second
	failed := 0
	var drained, moved time.Time
	for {
		wait := activeInterval
		pulling, err := r.pulling(ctx)
		switch {
		case err != nil:
			report(err)
			wait = leadInterval
		case !pulling:
			wait = leadInterval
		default:
			if r.Peer == nil && time.Since(moved) >= moveInterval {
				moved = time.Now()
				if err := r.Move(ctx); errors.Is(err, client.ErrNotHome) {
					log.Warnf("sync: %v, keeping the prophet the disciple is bound to", err)
				} else {
					report(err)
				}
			}
			_, err := r.PullSync(ctx)
			switch {
			case errors.Is(err, client.ErrNotHome):
				report(err)
				wait = backoff
				backoff = min(backoff*2, maxBackoff)
			case errors.Is(err, ErrSnapshotRequired):
				if err := r.PullSnapshot(ctx, "", r.reportStalledCursor(ctx, "was refused by the prophet feed")); err != nil {
					report(err)
					wait = backoff
					backoff = min(backoff*2, maxBackoff)
				} else {
					backoff = time.Second
					failed = 0
				}
			case err != nil:
				report(err)
				wait = backoff
				backoff = min(backoff*2, maxBackoff)
				failed++
				if failed >= failedPullsBeforeReconcile {
					failed = 0
					reason := fmt.Sprintf("failed to apply %d consecutive pulls, last error %v", failedPullsBeforeReconcile, err)
					if err := r.PullSnapshot(ctx, "", r.reportStalledCursor(ctx, reason)); err != nil {
						report(err)
					} else {
						backoff = time.Second
					}
				}
			default:
				backoff = time.Second
				failed = 0
			}
		}
		if r.Peer == nil && time.Since(drained) >= drainInterval {
			drained = time.Now()
			if _, err := r.DB.PostAccessDiffDrain(ctx, drainPairs); err != nil {
				report(fmt.Errorf("drain access_diff: %w", err))
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (r *Replica) pulling(ctx context.Context) (bool, error) {
	if r.Peer == nil {
		return true, nil
	}
	if _, err := r.GetState(ctx); err != nil {
		return false, fmt.Errorf("read peer %s: %w", r.Peer.Prophet, err)
	}
	if r.Peer.Status != "active" {
		return false, nil
	}
	var home sql.NullString
	err := r.DB.NewRaw("SELECT claimius.get_app_home(id) FROM claimius.samna_app WHERE slug = ?", r.Peer.AppSlug).Scan(ctx, &home)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("read home of %s: %w", r.Peer.AppSlug, err)
	}
	return home.String == r.Peer.Prophet, nil
}

func (r *Replica) Move(ctx context.Context) error {
	state, err := r.GetState(ctx)
	if err != nil {
		return fmt.Errorf("get current seq: %w", err)
	}
	configured := utils.Cfg().Endpoint
	if r.Peer != nil || state.Endpoint == "" || state.Endpoint == configured {
		return nil
	}
	return r.PullSnapshot(ctx, configured, fmt.Sprintf("moved from prophet %s at %s to %s", state.Prophet, state.Endpoint, configured))
}

type SyncPayload struct {
	client.SyncCursor
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

func (r *Replica) ApplyPage(ctx context.Context, from client.SyncCursor, to State, payloads []SyncPayload, reconcile string) error {
	return r.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx, "SET LOCAL claimius.replay_mode = 'true'"); err != nil {
			return fmt.Errorf("set replay_mode: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "SET LOCAL synchronous_commit = off"); err != nil {
			return fmt.Errorf("set synchronous_commit: %w", err)
		}
		if err := r.UpdateSeq(ctx, tx, from, to); err != nil {
			return err
		}
		for _, payload := range payloads {
			var err error
			if r.Peer != nil {
				err = r.applyPeerPayload(ctx, tx, payload)
			} else {
				err = ApplyPayload(ctx, tx, payload)
			}
			if err != nil {
				return fmt.Errorf("apply %s seq %s: %w", payload.EventType, payload.SyncCursor, err)
			}
		}
		if reconcile == "" {
			return nil
		}
		if r.Peer != nil {
			var appID uuid.UUID
			if err := tx.NewRaw("SELECT id FROM claimius.samna_app WHERE slug = ?", r.Peer.AppSlug).Scan(ctx, &appID); err != nil {
				return fmt.Errorf("read mirrored app %s: %w", r.Peer.AppSlug, err)
			}
			return shared.WriteAudit(ctx, tx, &appID, "peer sync reconcile", "update", "claimius.prophet_peer", r.Peer.ID, uuid.Nil, nil, reconcile)
		}
		return sdb.Write(ctx, tx, "disciple sync reconcile", "update", "claimius.disciple_state", uuid.Nil, uuid.Nil, nil, reconcile)
	})
}

func (r *Replica) applyPeerPayload(ctx context.Context, tx bun.Tx, payload SyncPayload) error {
	var applied bool
	if err := tx.NewRaw("SELECT claimius.apply_peer_event(?, ?, ?, ?, ?)",
		r.Peer.Prophet, r.Peer.AppSlug, payload.Operation, payload.EventType, string(payload.Payload)).Scan(ctx, &applied); err != nil {
		return err
	}
	if !applied {
		r.DB.Logger.Warnf("peer sync: %s %s from prophet %s refused, recorded in claimius.peer_collision",
			payload.EventType, payload.SyncCursor, r.Peer.Prophet)
	}
	return nil
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

	return shared.RecomputeExternal(ctx, tx, payload.EventType, payload.Payload)
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
		return store.Upsert(ctx, tx, nil, &incoming)
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
	case "claimius.object_asset":
		return upsertEntity[shared.ObjectAsset](ctx, tx, raw)
	case "claimius.user_token":
		return upsertEntity[shared.UserToken](ctx, tx, raw)
	default:
		return false, fmt.Errorf("unknown event_type for upsert: %s", eventType)
	}
}

func upsertEntity[T any](ctx context.Context, tx bun.Tx, raw json.RawMessage) (bool, error) {
	var entity T
	if err := sauth.Unmarshal(raw, &entity); err != nil {
		return false, fmt.Errorf("unmarshal: %w", err)
	}
	return store.Upsert(ctx, tx, nil, &entity)
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
	case "claimius.object_asset":
		return deleteEntity[shared.ObjectAsset](ctx, tx, env.ID)
	case "claimius.user_token":
		return deleteEntity[shared.UserToken](ctx, tx, env.ID)
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

func (r *Replica) UpdateSeq(ctx context.Context, tx bun.Tx, from client.SyncCursor, to State) error {
	q := tx.NewUpdate()
	if r.Peer != nil {
		q = q.Model((*shared.ProphetPeer)(nil)).Where("id = ?", r.Peer.ID)
	} else {
		q = q.Model((*shared.DiscipleState)(nil))
		if to.Prophet != "" {
			q = q.Set("prophet = ?", to.Prophet).Set("endpoint = ?", to.Endpoint)
		}
	}
	result, err := q.
		Set("last_applied_txid = ?", to.Cursor.Txid).
		Set("last_applied_seq = ?", to.Cursor.Seq).
		Set("sa_updated_at = ?", time.Now()).
		Where("last_applied_txid = ?", from.Txid).
		Where("last_applied_seq = ?", from.Seq).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("update seq: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update seq rows: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("update seq: cursor moved from %s before the page applied", from)
	}
	return nil
}

func (r *Replica) GetState(ctx context.Context) (State, error) {
	if r.Peer != nil {
		var peer shared.ProphetPeer
		if err := r.DB.NewSelect().Model(&peer).Where("id = ?", r.Peer.ID).Scan(ctx); err != nil {
			return State{}, err
		}
		*r.Peer = peer
		return State{
			ID:       peer.ID,
			Cursor:   client.SyncCursor{Txid: peer.LastAppliedTxid, Seq: peer.LastAppliedSeq},
			Prophet:  peer.Prophet,
			Endpoint: peer.Endpoint,
		}, nil
	}
	var row shared.DiscipleState
	err := r.DB.NewSelect().
		Model(&row).
		Limit(1).
		Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return State{}, nil
	}
	if err != nil {
		return State{}, err
	}
	state := State{ID: row.ID, Cursor: row.Cursor()}
	if row.Prophet != nil {
		state.Prophet = *row.Prophet
	}
	if row.Endpoint != nil {
		state.Endpoint = *row.Endpoint
	}
	return state, nil
}
