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

func PostSync(ctx echo.Context) error {
	appID, ok := ctx.Get("app_id").(uuid.UUID)
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "Missing app context")
	}

	var rows []UserObject
	if err := ctx.Bind(&rows); err != nil {
		ctx.Logger().Errorf("sync: bind: %v", err)
		return respond.Error(ctx, http.StatusBadRequest, "Malformed request")
	}

	if len(rows) == 0 {
		return respond.JSON(ctx, http.StatusOK, map[string]any{"accepted": 0}, 1)
	}

	tx, err := sdb.Instance().BeginTx(ctx.Request().Context(), nil)
	if err != nil {
		ctx.Logger().Errorf("sync: begin tx: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx.Request().Context(), "SET LOCAL claimius.replay_mode = 'true'"); err != nil {
		ctx.Logger().Errorf("sync: set replay_mode: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	accepted := 0
	for _, row := range rows {
		if row.ID == uuid.Nil || row.UserID == uuid.Nil || row.ObjectID == uuid.Nil {
			continue
		}

		_, err := tx.NewInsert().
			Model(&row).
			On("CONFLICT (id) DO UPDATE").
			Set("app_id = EXCLUDED.app_id").
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
			Set("search_vector = EXCLUDED.search_vector").
			Set("sa_updated_at = CURRENT_TIMESTAMP").
			Exec(ctx.Request().Context())
		if err != nil {
			ctx.Logger().Errorf("sync: upsert: %v", err)
			continue
		}
		accepted++
	}

	if err := tx.Commit(); err != nil {
		ctx.Logger().Errorf("sync: commit: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	return respond.JSON(ctx, http.StatusOK, map[string]any{
		"accepted": accepted,
		"app_id":   appID,
	}, 1)
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
		{"claimius.audit", &[]Audit{}, true},
		{"claimius.user_object", &[]UserObject{}, true},
		{"claimius.object_users", &[]ObjectUsers{}, true},
		{"claimius.user_users", &[]UserUsers{}, true},
	}

	var records []SyncPayload

	for _, t := range tables {
		q := sdb.Instance().NewSelect().Model(t.model)
		if t.filterApp && appID != uuid.Nil {
			q = q.Where("app_id = ?", appID)
		}
		if err := q.Scan(ctx.Request().Context()); err != nil {
			ctx.Logger().Errorf("snapshot %s: %v", t.eventType, err)
			continue
		}

		items, err := sauth.MarshalRows(t.model)
		if err != nil {
			continue
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
	err := sdb.Instance().NewSelect().
		Model(&events).
		Where("seq > ?", seq).
		Where("(app_id = ? OR app_id IS NULL)", appID).
		OrderExpr("seq ASC").
		Limit(1000).
		Scan(ctx.Request().Context())

	if err != nil {
		ctx.Logger().Errorf("sync events: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	maxSeq := seq
	payloads := make([]SyncPayload, 0, len(events))
	for _, event := range events {
		payloads = append(payloads, SyncPayload{
			Seq:       event.Seq,
			Operation: event.Operation,
			EventType: event.EventType,
			Payload:   event.Payload,
		})
		if event.Seq > maxSeq {
			maxSeq = event.Seq
		}
	}

	if err := syncArmPush(sdb.Instance(), ctx.Request().Context(), appID, maxSeq); err != nil {
		ctx.Logger().Errorf("delta arm push %s: %v", appID, err)
	}

	return respond.JSON(ctx, http.StatusOK, payloads, len(payloads))
}
