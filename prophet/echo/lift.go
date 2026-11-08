package echo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/lib/pq"
	"github.com/nimling/samna-auth-middleware/v2/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/sauth"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/driver/pgdriver"
)

type LiftRequest struct {
	Records []SyncPayload `json:"records"`
}

type LiftCollision struct {
	EventType string    `json:"event_type"`
	ID        uuid.UUID `json:"id"`
}

type LiftResult struct {
	Lifted     map[string]int  `json:"lifted"`
	Collisions []LiftCollision `json:"collisions,omitempty"`
	Message    string          `json:"message,omitempty"`
}

var liftOrder = []string{
	"claimius.organization",
	"claimius.location",
	"claimius.samna_user",
	"claimius.samna_client",
	"claimius.samna_secret",
	"claimius.claim",
	"claimius.user_claim",
	"claimius.claim_object",
	"claimius.user_token",
	"claimius.user_relation",
	"claimius.object_field",
	"claimius.user_field",
	"claimius.object_asset",
}

var errLiftCollision = errors.New("lift collision")

func liftModel(eventType string) any {
	switch eventType {
	case "claimius.organization":
		return &shared.Organization{}
	case "claimius.location":
		return &shared.Location{}
	case "claimius.samna_user":
		return &shared.SamnaUser{}
	case "claimius.samna_client":
		return &shared.SamnaClient{}
	case "claimius.samna_secret":
		return &shared.SamnaSecret{}
	case "claimius.claim":
		return &shared.Claim{}
	case "claimius.user_claim":
		return &shared.UserClaim{}
	case "claimius.claim_object":
		return &shared.ClaimObject{}
	case "claimius.user_token":
		return &shared.UserToken{}
	case "claimius.user_relation":
		return &shared.UserRelation{}
	case "claimius.object_field":
		return &shared.ObjectField{}
	case "claimius.user_field":
		return &shared.UserField{}
	case "claimius.object_asset":
		return &shared.ObjectAsset{}
	}
	return nil
}

func PostLift(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	if appID == uuid.Nil {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	if refused, err := checkSyncHome(ctx, appID); refused {
		return err
	}
	var req LiftRequest
	if err := ctx.Bind(&req); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	byType := map[string][]json.RawMessage{}
	for _, r := range req.Records {
		if liftModel(r.EventType) == nil {
			return respond.Error(ctx, http.StatusBadRequest, "unknown event_type "+r.EventType)
		}
		byType[r.EventType] = append(byType[r.EventType], r.Payload)
	}
	result := LiftResult{Lifted: map[string]int{}}
	err := sdb.Instance().RunInTx(ctx.Request().Context(), nil, func(c context.Context, tx bun.Tx) error {
		for _, t := range liftOrder {
			ids := make([]uuid.UUID, 0, len(byType[t]))
			for _, raw := range byType[t] {
				var row struct {
					ID uuid.UUID `json:"id"`
				}
				if err := json.Unmarshal(raw, &row); err != nil || row.ID == uuid.Nil {
					return echo.NewHTTPError(http.StatusBadRequest, t+" row without id")
				}
				ids = append(ids, row.ID)
			}
			if len(ids) == 0 {
				continue
			}
			hits := make([]uuid.UUID, 0)
			if err := tx.NewRaw("SELECT id FROM ? WHERE id = ANY(?)", bun.Safe(t), pq.Array(ids)).Scan(c, &hits); err != nil {
				return err
			}
			for _, id := range hits {
				result.Collisions = append(result.Collisions, LiftCollision{EventType: t, ID: id})
			}
		}
		if len(result.Collisions) > 0 {
			return errLiftCollision
		}
		for _, t := range liftOrder {
			for _, raw := range byType[t] {
				model := liftModel(t)
				if err := sauth.Unmarshal(raw, model); err != nil {
					return echo.NewHTTPError(http.StatusBadRequest, t+": "+err.Error())
				}
				q := tx.NewInsert().Model(model)
				if t != "claimius.organization" && t != "claimius.location" {
					if field := reflect.ValueOf(model).Elem().FieldByName("AppID"); field.IsValid() {
						switch field.Kind() {
						case reflect.Pointer:
							id := appID
							field.Set(reflect.ValueOf(&id))
						default:
							field.Set(reflect.ValueOf(appID))
						}
					} else {
						q = q.Value("app_id", "?", appID)
					}
				}
				if _, err := q.Exec(c); err != nil {
					return err
				}
				if t == "claimius.organization" || t == "claimius.location" {
					var row struct {
						ID        uuid.UUID  `json:"id"`
						CreatedBy *uuid.UUID `json:"sa_created_by"`
					}
					_ = json.Unmarshal(raw, &row)
					if _, err := tx.ExecContext(c, "SELECT claimius.upsert_app_object(?, ?, ?, ?)", appID, t, row.ID, row.CreatedBy); err != nil {
						return err
					}
				}
				result.Lifted[t]++
			}
		}
		return nil
	})
	if errors.Is(err, errLiftCollision) {
		result.Lifted = map[string]int{}
		result.Message = "ids already present on the prophet, nothing lifted"
		return respond.JSON(ctx, http.StatusConflict, result, len(result.Collisions))
	}
	var httpErr *echo.HTTPError
	if errors.As(err, &httpErr) {
		return respond.Error(ctx, httpErr.Code, httpErr.Message.(string))
	}
	var pgErr pgdriver.Error
	if errors.As(err, &pgErr) && pgErr.Field('C') == "23505" {
		return respond.Error(ctx, http.StatusConflict, "a lifted row collides with a row the prophet holds: "+pgErr.Field('n'))
	}
	if err != nil {
		ctx.Logger().Errorf("PostLift: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, result, 1)
}
