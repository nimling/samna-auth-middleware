package echo

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/sauth"
	"github.com/nimling/samna-auth-middleware/shared"
)

func syncArmPush(d *shared.DB, ctx context.Context, appID uuid.UUID, seq int64) error {
	now := time.Now()
	_, err := d.NewInsert().
		Model(&ProphetState{AppID: appID, LastAppliedSeq: seq, SyncMode: SYNC_MODE_ACTIVE}).
		On("CONFLICT (app_id) DO UPDATE").
		Set("last_applied_seq = EXCLUDED.last_applied_seq").
		Set("sync_mode = ?", SYNC_MODE_ACTIVE).
		Set("sync_failures = 0").
		Set("last_contact_at = ?", now).
		Set("next_attempt_at = ?", now).
		Set("sa_updated_at = ?", now).
		Exec(ctx)
	return err
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

func GetSyncSnapshot(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)

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

	for _, t := range tables {
		q := sdb.Instance().NewSelect().Model(t.model)
		if t.eventType == "claimius.organization" || t.eventType == "claimius.location" {
			if appID != uuid.Nil {
				q = q.Where("id IN (SELECT claimius.app_object_ids(?, ?))", appID, t.eventType)
			}
		} else if t.filterApp && appID != uuid.Nil {
			q = q.Where("app_id = ?", appID)
		}
		if err := q.Scan(ctx.Request().Context()); err != nil {
			ctx.Logger().Errorf("snapshot %s: %v", t.eventType, err)
			return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
		}

		items, err := sauth.MarshalRows(t.model)
		if err != nil {
			ctx.Logger().Errorf("snapshot marshal %s: %v", t.eventType, err)
			return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
		}

		for _, item := range items {
			records = append(records, SyncPayload{
				Operation: "upsert",
				EventType: t.eventType,
				Payload:   item,
			})
		}
	}

	var lastSeq int64
	if err := sdb.Instance().NewSelect().
		TableExpr("claimius.sync_event").
		ColumnExpr("COALESCE(MAX(seq), 0)").
		Scan(ctx.Request().Context(), &lastSeq); err != nil {
		lastSeq = 0
	}

	if records == nil {
		records = []SyncPayload{}
	}

	if appID != uuid.Nil {
		if err := syncArmPush(sdb.Instance(), ctx.Request().Context(), appID, lastSeq); err != nil {
			ctx.Logger().Errorf("snapshot arm push %s: %v", appID, err)
		}
	}

	return respond.JSON(ctx, http.StatusOK, SyncSnapshotResponse{
		Records: records,
		Seq:     lastSeq,
	}, 1)
}

func GetSyncEvents(ctx echo.Context) error {
	seq := int64(0)
	if err := echo.PathParamsBinder(ctx).Int64("seq", &seq).BindError(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Bad request")
	}

	appID, _ := ctx.Get("app_id").(uuid.UUID)
	if appID == uuid.Nil {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}

	var events []SyncEvent
	err := sdb.Instance().NewRaw(
		"SELECT * FROM claimius.get_sync_events(?, ?, 1000)",
		appID, seq,
	).Scan(ctx.Request().Context(), &events)

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

	// The incoming seq is the disciple's proof of application: it only advances
	// its cursor after every payload applied. What was served is not proof, and
	// a disciple that fails to apply re-requests from the same point, so arming
	// on the served position would report consumption that never happened and
	// let the trim delete events still owed.
	if err := syncArmPush(sdb.Instance(), ctx.Request().Context(), appID, seq); err != nil {
		ctx.Logger().Errorf("delta arm push %s: %v", appID, err)
	}

	return respond.JSON(ctx, http.StatusOK, payloads, len(payloads))
}
