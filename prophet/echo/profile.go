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

func PutProfileItem(ctx echo.Context) error {
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	var body shared.BodySamProfileItem
	if err := ctx.Bind(&body); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := body.Validate(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	profile, err := sdb.PutProfileItem(ctx.Request().Context(), appID, body)
	if err != nil {
		return respondProfileError(ctx, err)
	}
	return respond.JSON(ctx, http.StatusOK, profile, 1)
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
	profile, err := sdb.DeleteProfileItem(ctx.Request().Context(), appID, body)
	if err != nil {
		return respondProfileError(ctx, err)
	}
	return respond.JSON(ctx, http.StatusOK, profile, 1)
}

func respondUserNameError(ctx echo.Context, err error) error {
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return respond.Error(ctx, http.StatusNotFound, "Not found")
	case errors.Is(err, shared.ErrBadUserName):
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	case errors.Is(err, shared.ErrUserNameTaken):
		return respond.Error(ctx, http.StatusConflict, err.Error())
	}
	ctx.Logger().Errorf("user name: %v", err)
	return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
}

func GetUserNameAvailable(ctx echo.Context) error {
	available, err := sdb.GetUserNameAvailable(ctx.Request().Context(), ctx.QueryParam("user_name"))
	if err != nil {
		return respondUserNameError(ctx, err)
	}
	return respond.JSON(ctx, http.StatusOK, available, 1)
}

func PutUserName(ctx echo.Context) error {
	var body shared.BodySamUserName
	if err := ctx.Bind(&body); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "invalid json body")
	}
	if err := body.Validate(); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	reserved, err := sdb.PutUserName(ctx.Request().Context(), body)
	if err != nil {
		return respondUserNameError(ctx, err)
	}
	return respond.JSON(ctx, http.StatusOK, reserved, 1)
}
