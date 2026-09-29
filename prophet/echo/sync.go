package echo

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	"github.com/nimling/samna-auth-middleware/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/sauth"
	"github.com/nimling/samna-auth-middleware/shared"
	"github.com/uptrace/bun"
)

const (
	SyncHoldMax      = 25 * time.Second
	syncChannel      = "claimius_sync"
	syncRetry        = 100 * time.Millisecond
	syncListenerPing = 90 * time.Second
)

// syncFeed wakes the pulls held open for an app when an event of the app
// commits. One LISTEN connection per process carries every app.
type syncFeed struct {
	mu      sync.Mutex
	waiters map[uuid.UUID]map[chan struct{}]struct{}
}

var (
	syncFeedOnce sync.Once
	syncFeedRun  *syncFeed
)

func getSyncFeed(d *shared.DB) *syncFeed {
	syncFeedOnce.Do(func() {
		listener := pq.NewListener(d.GetConnectionString(), time.Second, time.Minute, func(_ pq.ListenerEventType, err error) {
			if err != nil {
				d.Logger.Errorf("sync listener: %v", err)
			}
		})
		if err := listener.Listen(syncChannel); err != nil {
			d.Logger.Errorf("sync listener: listen %s: %v", syncChannel, err)
			_ = listener.Close()
			return
		}
		syncFeedRun = &syncFeed{waiters: make(map[uuid.UUID]map[chan struct{}]struct{})}
		go syncFeedRun.listen(d, listener)
	})
	return syncFeedRun
}

func (f *syncFeed) listen(d *shared.DB, listener *pq.Listener) {
	for {
		select {
		case n := <-listener.Notify:
			if n == nil {
				f.wake(nil)
				continue
			}
			if appID, err := uuid.Parse(n.Extra); err == nil {
				f.wake(&appID)
			}
		case <-time.After(syncListenerPing):
			go func() {
				if err := listener.Ping(); err != nil {
					d.Logger.Errorf("sync listener ping failed: %v", err)
				}
			}()
		}
	}
}

func (f *syncFeed) subscribe(appID uuid.UUID) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	f.mu.Lock()
	if f.waiters[appID] == nil {
		f.waiters[appID] = make(map[chan struct{}]struct{})
	}
	f.waiters[appID][ch] = struct{}{}
	f.mu.Unlock()
	return ch, func() {
		f.mu.Lock()
		delete(f.waiters[appID], ch)
		if len(f.waiters[appID]) == 0 {
			delete(f.waiters, appID)
		}
		f.mu.Unlock()
	}
}

func (f *syncFeed) wake(appID *uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, set := range f.waiters {
		if appID != nil && id != *appID {
			continue
		}
		for ch := range set {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
}

type SyncPayload struct {
	Seq       int64           `json:"seq"`
	Operation string          `json:"operation"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

type SyncSnapshotResponse struct {
	Records []SyncPayload `json:"records"`
	Seq     int64         `json:"seq"`
}

func getSyncConsumer(ctx echo.Context) (uuid.UUID, error) {
	raw := ctx.QueryParam("consumer")
	if raw == "" {
		return uuid.Nil, nil
	}
	return uuid.Parse(raw)
}

func GetSyncSnapshot(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	consumerID, err := getSyncConsumer(ctx)
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Bad request")
	}

	tables := []struct {
		eventType string
		model     any
		filterApp bool
	}{
		{"claimius.samna_user", &[]SamnaUser{}, true},
		{"claimius.organization", &[]Organization{}, true},
		{"claimius.location", &[]Location{}, true},
		{"claimius.claim", &[]Claim{}, true},
		{"claimius.user_claim", &[]UserClaim{}, true},
		{"claimius.claim_object", &[]ClaimObject{}, true},
		{"claimius.samna_client", &[]SamnaClient{}, true},
		{"claimius.samna_secret", &[]SamnaSecret{}, true},
		{"claimius.object_field", &[]ObjectField{}, true},
		{"claimius.user_field", &[]UserField{}, true},
		{"claimius.user_relation", &[]UserRelation{}, true},
	}

	var records []SyncPayload
	var lastSeq int64

	reqCtx := ctx.Request().Context()
	err = sdb.Instance().RunInTx(reqCtx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}, func(txCtx context.Context, tx bun.Tx) error {
		for _, t := range tables {
			q := tx.NewSelect().Model(t.model)
			if t.eventType == "claimius.organization" || t.eventType == "claimius.location" {
				if appID != uuid.Nil {
					q = q.Where("id IN (SELECT claimius.app_object_ids(?, ?))", appID, t.eventType)
				}
			} else if t.filterApp && appID != uuid.Nil {
				q = q.Where("app_id = ?", appID)
			}
			if err := q.Scan(txCtx); err != nil {
				ctx.Logger().Errorf("snapshot %s: %v", t.eventType, err)
				return err
			}

			items, err := sauth.MarshalRows(t.model)
			if err != nil {
				ctx.Logger().Errorf("snapshot marshal %s: %v", t.eventType, err)
				return err
			}

			for _, item := range items {
				records = append(records, SyncPayload{
					Operation: "upsert",
					EventType: t.eventType,
					Payload:   item,
				})
			}
		}

		seq := tx.NewSelect().
			TableExpr("claimius.sync_event AS e").
			ColumnExpr("COALESCE(MAX(e.seq), 0)").
			Where("e.txid < pg_snapshot_xmin(pg_current_snapshot())")
		if appID != uuid.Nil {
			seq = seq.Where("e.app_id = ?", appID)
		}
		return seq.Scan(txCtx, &lastSeq)
	})
	if err != nil {
		ctx.Logger().Errorf("snapshot %s: %v", appID, err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	if records == nil {
		records = []SyncPayload{}
	}

	if appID != uuid.Nil {
		if _, err := sdb.Instance().NewRaw("SELECT claimius.update_sync_cursor(?, ?, ?)", appID, consumerID, lastSeq).
			Exec(reqCtx); err != nil {
			ctx.Logger().Errorf("snapshot cursor %s: %v", appID, err)
		}
	}

	return respond.JSON(ctx, http.StatusOK, SyncSnapshotResponse{
		Records: records,
		Seq:     lastSeq,
	}, 1)
}

// GetSyncEvents serves the page of the app's events after seq. With wait set
// it holds an empty answer open up to wait seconds, at most SyncHoldMax, and
// answers the moment an event of the app commits. A cursor the feed cannot
// serve answers 409 and the consumer takes the snapshot.
func GetSyncEvents(ctx echo.Context) error {
	seq := int64(0)
	if err := echo.PathParamsBinder(ctx).Int64("seq", &seq).BindError(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Bad request")
	}
	wait := 0
	if err := echo.QueryParamsBinder(ctx).Int("wait", &wait).BindError(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Bad request")
	}
	consumerID, err := getSyncConsumer(ctx)
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Bad request")
	}

	appID, _ := ctx.Get("app_id").(uuid.UUID)
	if appID == uuid.Nil {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}

	reqCtx := ctx.Request().Context()
	var stale sql.NullString
	if err := sdb.Instance().NewRaw("SELECT claimius.check_sync_cursor(?, ?)", appID, seq).Scan(reqCtx, &stale); err != nil {
		ctx.Logger().Errorf("sync cursor: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	if stale.Valid {
		return respond.Error(ctx, http.StatusConflict, "Snapshot required: cursor "+stale.String)
	}

	// The incoming seq is the disciple's proof of application: it only advances
	// its cursor after every payload applied. What was served is not proof, and
	// a disciple that fails to apply re-requests from the same point, so arming
	// on the served position would report consumption that never happened and
	// let the trim delete events still owed.
	if _, err := sdb.Instance().NewRaw("SELECT claimius.update_sync_cursor(?, ?, ?)", appID, consumerID, seq).
		Exec(reqCtx); err != nil {
		ctx.Logger().Errorf("delta cursor %s: %v", appID, err)
	}

	hold := time.Duration(wait) * time.Second
	if hold > SyncHoldMax {
		hold = SyncHoldMax
	}
	events, err := getSyncPage(reqCtx, sdb.Instance(), appID, seq, hold)
	if err != nil {
		ctx.Logger().Errorf("sync events: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	payloads := make([]SyncPayload, 0, len(events))
	for _, event := range events {
		payloads = append(payloads, SyncPayload{
			Seq:       event.Seq,
			Operation: event.Operation,
			EventType: event.EventType,
			Payload:   event.Payload,
		})
	}

	return respond.JSON(ctx, http.StatusOK, payloads, len(payloads))
}

func getSyncPage(ctx context.Context, d *shared.DB, appID uuid.UUID, seq int64, hold time.Duration) ([]SyncEvent, error) {
	var wake <-chan struct{}
	if hold > 0 {
		if feed := getSyncFeed(d); feed != nil {
			ch, stop := feed.subscribe(appID)
			defer stop()
			wake = ch
		}
	}
	deadline := time.NewTimer(hold)
	defer deadline.Stop()
	pending := false
	for {
		var events []SyncEvent
		if err := d.NewRaw("SELECT * FROM claimius.get_sync_events(?, ?, 1000)", appID, seq).Scan(ctx, &events); err != nil {
			return nil, err
		}
		if len(events) > 0 || wake == nil {
			return events, nil
		}
		var again <-chan time.Time
		if pending {
			again = time.After(syncRetry)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return events, nil
		case <-wake:
			pending = true
		case <-again:
		}
	}
}
