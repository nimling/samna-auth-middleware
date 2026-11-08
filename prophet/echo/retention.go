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

func respondRetentionError(ctx echo.Context, err error) error {
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return respond.Error(ctx, http.StatusNotFound, "Not found")
	case errors.Is(err, shared.ErrNoAccess):
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	case errors.Is(err, shared.ErrBadRetention):
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	ctx.Logger().Errorf("retention: %v", err)
	return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
}

func GetRetentions(ctx echo.Context) error {
	userID, ok := ctx.Get("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	var allowed bool
	if err := sdb.Instance().NewRaw(
		`SELECT EXISTS (SELECT 1 FROM claimius.get_app() a, claimius.get_access(?, a.id, a.sa_owner_id, ?, ?))`,
		userID, shared.OBJECT_TYPE_ORGANIZATION, int(shared.ACCESS_READ),
	).Scan(ctx.Request().Context(), &allowed); err != nil {
		return respondRetentionError(ctx, err)
	}
	if !allowed {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
	items, err := sdb.Instance().GetRetentions(ctx.Request().Context())
	if err != nil {
		return respondRetentionError(ctx, err)
	}
	return respond.JSON(ctx, http.StatusOK, items, len(items))
}

func PutRetention(ctx echo.Context) error {
	userID, ok := ctx.Get("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	var body shared.BodyRetention
	if err := ctx.Bind(&body); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "days must be a number")
	}
	var allowed bool
	if err := sdb.Instance().NewRaw(
		`SELECT EXISTS (SELECT 1 FROM claimius.get_app() a, claimius.get_access(?, a.id, a.sa_owner_id, ?, ?))`,
		userID, shared.OBJECT_TYPE_ORGANIZATION, int(shared.ACCESS_WRITE),
	).Scan(ctx.Request().Context(), &allowed); err != nil {
		return respondRetentionError(ctx, err)
	}
	if !allowed {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
	item, err := sdb.Instance().PutRetention(ctx.Request().Context(), ctx.Param("kind"), body.Days)
	if err != nil {
		return respondRetentionError(ctx, err)
	}
	return respond.JSON(ctx, http.StatusOK, item, 1)
}
