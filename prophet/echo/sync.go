package echo

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	"github.com/nimling/samna-auth-middleware/v2/client"
	"github.com/nimling/samna-auth-middleware/v2/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/sauth"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/uptrace/bun"
)

const (
	SyncHoldMax = 25 * time.Second
	syncChannel = "claimius_sync"
	syncRetry   = 100 * time.Millisecond
)

var serveCtx, EndStreams = context.WithCancel(context.Background())

type SyncPayload struct {
	client.SyncCursor
	Operation string          `json:"operation"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

type SyncSnapshotResponse struct {
	Records []SyncPayload `json:"records"`
	client.SyncCursor
}

func checkSyncHome(ctx echo.Context, appID uuid.UUID) (bool, error) {
	var home struct {
		Prophet string         `bun:"prophet"`
		Home    sql.NullString `bun:"home"`
	}
	if err := sdb.Instance().NewRaw("SELECT (SELECT slug FROM claimius.get_app()) AS prophet, claimius.check_sync_home(?, ?) AS home",
		appID, ctx.QueryParam("prophet")).Scan(ctx.Request().Context(), &home); err != nil {
		ctx.Logger().Errorf("sync home %s: %v", appID, err)
		return true, respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	if home.Home.Valid {
		ctx.Response().Header().Set(client.HeaderHome, home.Home.String)
		return true, respond.Error(ctx, http.StatusConflict, "The app's feed is served to this consumer by prophet "+home.Home.String)
	}
	ctx.Response().Header().Set(client.HeaderProphet, home.Prophet)
	return false, nil
}

func checkPeerGrant(ctx echo.Context, appID uuid.UUID, peer bool) (bool, error) {
	if !peer {
		return false, nil
	}
	var granted bool
	if err := sdb.Instance().NewRaw("SELECT EXISTS (SELECT 1 FROM claimius.get_peer_grant(?, ?))",
		appID, ctx.Request().Header.Get(client.HeaderPeerSecret)).Scan(ctx.Request().Context(), &granted); err != nil {
		ctx.Logger().Errorf("peer grant %s: %v", appID, err)
		return true, respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	if !granted {
		return true, respond.Error(ctx, http.StatusForbidden, "No peer grant for this app")
	}
	return false, nil
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
	peer := false
	if err := echo.QueryParamsBinder(ctx).Bool("peer", &peer).BindError(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Bad request")
	}
	if refused, err := checkPeerGrant(ctx, appID, peer); refused {
		return err
	}
	if appID != uuid.Nil {
		if refused, err := checkSyncHome(ctx, appID); refused {
			return err
		}
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
		{"claimius.object_asset", &[]shared.ObjectAsset{}, true},
		{shared.OBJECT_TYPE_APP_COST, &[]shared.AppCost{}, true},
		{shared.OBJECT_TYPE_CODE, &[]shared.Code{}, true},
		{"claimius.user_identity", &[]UserIdentity{}, false},
		{"claimius.audit", &[]shared.Audit{}, true},
	}

	var records []SyncPayload
	var cursor client.SyncCursor

	reqCtx := ctx.Request().Context()
	err = sdb.Instance().RunInTx(reqCtx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}, func(txCtx context.Context, tx bun.Tx) error {
		if peer {
			if err := tx.NewRaw("SELECT 'upsert' AS operation, event_type, payload FROM claimius.get_peer_snapshot(?)", appID).
				Scan(txCtx, &records); err != nil {
				ctx.Logger().Errorf("peer snapshot %s: %v", appID, err)
				return err
			}
		} else {
			for _, t := range tables {
				q := tx.NewSelect().Model(t.model)
				if t.eventType == string(OBJECT_TYPE_ORGANIZATION) || t.eventType == string(OBJECT_TYPE_LOCATION) {
					if appID != uuid.Nil {
						q = q.Where("id IN (SELECT claimius.app_object_ids(?, ?))", appID, t.eventType)
					}
					q = q.Order("sa_level", "id")
				} else if t.eventType == shared.OBJECT_TYPE_OBJECT_ASSET {
					if appID != uuid.Nil {
						q = q.Where("? IN (SELECT claimius.apps_for_object(?TableAlias.object_type, ?TableAlias.object_id, ?TableAlias.app_id))", appID)
					}
					q = q.Order("sa_created_at", "id")
				} else if t.eventType == shared.OBJECT_TYPE_APP_COST {
					if appID != uuid.Nil {
						q = q.Where("app_id = ?", appID)
					}
					q = q.Order("sa_created_at", "id")
				} else if t.filterApp && appID != uuid.Nil {
					q = q.Where("app_id = ?", appID)
				}
				if t.eventType == "claimius.user_identity" {
					q = q.ExcludeColumn("data")
					if appID != uuid.Nil {
						q = q.Where("? IN (SELECT claimius.apps_for_object('claimius.organization', ?TableAlias.sa_owner_id))", appID)
					}
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
		}

		return tx.NewRaw("SELECT pg_snapshot_xmin(pg_current_snapshot())::TEXT::BIGINT AS txid, 0::BIGINT AS seq").
			Scan(txCtx, &cursor)
	})
	if err != nil {
		ctx.Logger().Errorf("snapshot %s: %v", appID, err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	if records == nil {
		records = []SyncPayload{}
	}

	if appID != uuid.Nil {
		if _, err := sdb.Instance().NewRaw("SELECT claimius.update_sync_cursor(?, ?, ?, ?, ?)", appID, consumerID, cursor.Txid, cursor.Seq, peer).
			Exec(reqCtx); err != nil {
			ctx.Logger().Errorf("snapshot cursor %s: %v", appID, err)
		}
	}

	return respond.JSON(ctx, http.StatusOK, SyncSnapshotResponse{
		Records:    records,
		SyncCursor: cursor,
	}, 1)
}

func GetSyncEvents(ctx echo.Context) error {
	seq, err := client.ParseSyncCursor(ctx.Param("cursor"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Bad request")
	}
	wait := 0
	peer := false
	if err := echo.QueryParamsBinder(ctx).Int("wait", &wait).Bool("peer", &peer).BindError(); err != nil {
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
	if refused, err := checkPeerGrant(ctx, appID, peer); refused {
		return err
	}
	if refused, err := checkSyncHome(ctx, appID); refused {
		return err
	}

	reqCtx := ctx.Request().Context()
	var stale sql.NullString
	if err := sdb.Instance().NewRaw("SELECT claimius.check_sync_cursor(?, ?, ?)", appID, seq.Txid, seq.Seq).Scan(reqCtx, &stale); err != nil {
		ctx.Logger().Errorf("sync cursor: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	if stale.Valid {
		return respond.Error(ctx, http.StatusConflict, "Snapshot required: cursor "+stale.String)
	}

	if _, err := sdb.Instance().NewRaw("SELECT claimius.update_sync_cursor(?, ?, ?, ?, ?)", appID, consumerID, seq.Txid, seq.Seq, peer).
		Exec(reqCtx); err != nil {
		ctx.Logger().Errorf("delta cursor %s: %v", appID, err)
	}

	hold := time.Duration(wait) * time.Second
	if hold > SyncHoldMax {
		hold = SyncHoldMax
	}
	events, err := getSyncPage(reqCtx, sdb.Instance(), appID, seq, hold, peer)
	if err != nil {
		ctx.Logger().Errorf("sync events: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	payloads := make([]SyncPayload, 0, len(events))
	for _, event := range events {
		payloads = append(payloads, SyncPayload{
			SyncCursor: client.SyncCursor{Txid: event.Txid, Seq: event.Seq},
			Operation:  event.Operation,
			EventType:  event.EventType,
			Payload:    event.Payload,
		})
	}

	return respond.JSON(ctx, http.StatusOK, payloads, len(payloads))
}

func getSyncPage(ctx context.Context, d *shared.DB, appID uuid.UUID, seq client.SyncCursor, hold time.Duration, peer bool) ([]SyncEvent, error) {
	var wake <-chan *pq.Notification
	if hold > 0 {
		listenCtx, stop := context.WithCancel(ctx)
		defer stop()
		ch, err := d.Listen(listenCtx, syncChannel)
		if err != nil {
			return nil, err
		}
		wake = ch
	}
	deadline := time.NewTimer(hold)
	defer deadline.Stop()
	pending := false
	for {
		var events []SyncEvent
		if err := d.NewRaw("SELECT * FROM claimius.get_sync_events(?, ?, ?, 1000, ?)", appID, seq.Txid, seq.Seq, peer).Scan(ctx, &events); err != nil {
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
		case <-serveCtx.Done():
			return events, nil
		case n := <-wake:
			if n == nil || n.Extra == appID.String() {
				pending = true
			}
		case <-again:
		}
	}
}
