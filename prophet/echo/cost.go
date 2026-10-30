package echo

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/pkg/errorsx"
	"github.com/nimling/samna-auth-middleware/v2/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

const appCostColumns = `id, module, period_start, quantity, unit, amount, currency, detail, sa_owner_id, sa_created_by, sa_deleted_at, sa_created_at, sa_updated_at`

func PostAppCost(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	cost := &shared.AppCost{}
	if err := cost.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := cost.ValidatePost(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	owner, err := getAppCostOwner(ctx, appID, cost.CreatedBy, true)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	cost.OwnerID = owner
	cost.DeletedAt = nil
	if len(cost.Detail) == 0 {
		cost.Detail = []byte(`{}`)
	}
	if _, err := sdb.Instance().DB.NewInsert().
		Model(cost).
		Value("app_id", "?", appID).
		On("CONFLICT (app_id, module, period_start) WHERE sa_deleted_at IS NULL DO UPDATE").
		Set("quantity = EXCLUDED.quantity").
		Set("unit = EXCLUDED.unit").
		Set("amount = EXCLUDED.amount").
		Set("currency = EXCLUDED.currency").
		Set("detail = EXCLUDED.detail").
		Set("sa_created_by = EXCLUDED.sa_created_by").
		Returning(appCostColumns).
		Exec(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("PostAppCost: %v", err)
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	status := http.StatusOK
	if cost.CreatedAt.Equal(cost.UpdatedAt) {
		status = http.StatusCreated
	}
	return respond.JSON(ctx, status, cost, 1)
}

func PutAppCost(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	costID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	patch := &shared.AppCost{}
	if err := patch.Bind(ctx); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := patch.ValidatePatch(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	cost, err := getAppCostWritable(ctx, appID, costID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	columns := make([]string, 0, 5)
	if patch.HasKey("quantity") {
		cost.Quantity = patch.Quantity
		columns = append(columns, "quantity")
	}
	if patch.HasKey("unit") {
		cost.Unit = patch.Unit
		columns = append(columns, "unit")
	}
	if patch.HasKey("amount") {
		cost.Amount = patch.Amount
		columns = append(columns, "amount")
	}
	if patch.HasKey("currency") {
		cost.Currency = patch.Currency
		columns = append(columns, "currency")
	}
	if patch.HasKey("detail") {
		cost.Detail = patch.Detail
		columns = append(columns, "detail")
	}
	if len(columns) == 0 {
		return respond.JSON(ctx, http.StatusOK, cost, 1)
	}
	if _, err := sdb.Instance().DB.NewUpdate().
		Model(cost).
		Column(columns...).
		Where("id = ?", costID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Returning(appCostColumns).
		Exec(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("PutAppCost: %v", err)
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusOK, cost, 1)
}

func DeleteAppCost(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	costID, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	cost, err := getAppCostWritable(ctx, appID, costID)
	if err != nil {
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	if _, err := sdb.Instance().DB.NewUpdate().
		Model(cost).
		Set("sa_deleted_at = now()").
		Where("id = ?", costID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Returning(appCostColumns).
		Exec(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("DeleteAppCost: %v", err)
		return respond.Error(ctx, errorsx.ErrToHttpCode(err), "")
	}
	return respond.JSON(ctx, http.StatusOK, cost, 1)
}

func getAppCostOwner(ctx echo.Context, appID uuid.UUID, createdBy *uuid.UUID, stamped bool) (uuid.UUID, error) {
	if createdBy == nil || *createdBy == uuid.Nil {
		return uuid.Nil, errorsx.ErrForbidden
	}
	actor, err := getObjectAssetActor(ctx, appID, createdBy)
	if err != nil {
		return uuid.Nil, err
	}
	var grant *uuid.UUID
	if stamped {
		grant = createdBy
	}
	var owner uuid.UUID
	if err := sdb.Instance().NewRaw(`
		SELECT a.sa_owner_id
		FROM claimius.samna_app a
		WHERE a.id = ?
		  AND a.sa_deleted_at IS NULL
		  AND EXISTS (
			SELECT 1 FROM claimius.user_claim uc
			WHERE uc.app_id = a.id AND uc.user_id = ? AND uc.sa_deleted_at IS NULL AND (?::uuid IS NULL OR uc.id = ?::uuid))`, appID, actor, grant, grant).
		Scan(ctx.Request().Context(), &owner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, errorsx.ErrForbidden
		}
		return uuid.Nil, errorsx.ErrDbUnexpected
	}
	return owner, nil
}

func getAppCostWritable(ctx echo.Context, appID, costID uuid.UUID) (*shared.AppCost, error) {
	cost := &shared.AppCost{}
	if err := sdb.Instance().DB.NewSelect().
		Model(cost).
		Where("id = ?", costID).
		Where("app_id = ?", appID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx.Request().Context()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errorsx.ErrNotFound
		}
		return nil, errorsx.ErrDbUnexpected
	}
	if _, err := getAppCostOwner(ctx, appID, cost.CreatedBy, false); err != nil {
		return nil, err
	}
	return cost, nil
}
