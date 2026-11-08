package echo

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/pkg/respond"

	sdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func respondProfileError(ctx echo.Context, err error) error {
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return respond.Error(ctx, http.StatusNotFound, "Not found")
	case errors.Is(err, shared.ErrBadProfileItem):
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	ctx.Logger().Errorf("profile: %v", err)
	return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
}

func GetProfileItems(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	userID, err := uuid.Parse(ctx.QueryParam("user_id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "user_id is required")
	}
	items, err := sdb.GetProfileItems(ctx.Request().Context(), userID, &appID)
	if err != nil {
		return respondProfileError(ctx, err)
	}
	return respond.JSON(ctx, http.StatusOK, items, len(items))
}

func PutProfileItem(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	var body shared.BodySamProfileItem
	if err := ctx.Bind(&body); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := body.Validate(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	item, err := sdb.PutProfileItem(ctx.Request().Context(), appID, body)
	if err != nil {
		return respondProfileError(ctx, err)
	}
	return respond.JSON(ctx, http.StatusOK, item, 1)
}

func DeleteProfileItem(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	var body shared.BodySamProfileItemKey
	if err := ctx.Bind(&body); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := body.Validate(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	items, err := sdb.DeleteProfileItem(ctx.Request().Context(), appID, body)
	if err != nil {
		return respondProfileError(ctx, err)
	}
	return respond.JSON(ctx, http.StatusOK, items, len(items))
}
