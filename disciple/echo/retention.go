package echo

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	sdb "github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func respondRetentionError(ctx echo.Context, err error) error {
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return respondError(ctx, http.StatusNotFound, "Not found")
	case errors.Is(err, shared.ErrNoAccess):
		return respondError(ctx, http.StatusForbidden, "Forbidden")
	case errors.Is(err, shared.ErrBadRetention):
		return respondError(ctx, http.StatusBadRequest, err.Error())
	}
	ctx.Logger().Errorf("retention: %v", err)
	return respondError(ctx, http.StatusInternalServerError, "Internal server error")
}

func GetRetentions(ctx echo.Context) error {
	userID, ok := ctx.Get("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return respondError(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	app, err := sdb.Instance().GetApp(ctx.Request().Context())
	if err != nil {
		return respondRetentionError(ctx, err)
	}
	if _, err := sdb.Instance().GetAccess(ctx.Request().Context(), userID, app.OwnerID, shared.OBJECT_TYPE_ORGANIZATION, shared.ACCESS_READ); err != nil {
		return respondRetentionError(ctx, err)
	}
	items, err := sdb.Instance().GetRetentions(ctx.Request().Context())
	if err != nil {
		return respondRetentionError(ctx, err)
	}
	return respondJSON(ctx, http.StatusOK, items, len(items))
}

func PutRetention(ctx echo.Context) error {
	userID, ok := ctx.Get("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return respondError(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	var body shared.BodyRetention
	if err := ctx.Bind(&body); err != nil {
		return respondError(ctx, http.StatusBadRequest, "days must be a number")
	}
	app, err := sdb.Instance().GetApp(ctx.Request().Context())
	if err != nil {
		return respondRetentionError(ctx, err)
	}
	if _, err := sdb.Instance().GetAccess(ctx.Request().Context(), userID, app.OwnerID, shared.OBJECT_TYPE_ORGANIZATION, shared.ACCESS_WRITE); err != nil {
		return respondRetentionError(ctx, err)
	}
	item, err := sdb.Instance().PutRetention(ctx.Request().Context(), ctx.Param("kind"), body.Days)
	if err != nil {
		return respondRetentionError(ctx, err)
	}
	return respondJSON(ctx, http.StatusOK, item, 1)
}
