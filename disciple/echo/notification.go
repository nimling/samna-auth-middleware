package echo

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"

	sdb "github.com/nimling/samna-auth-middleware/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/notification"
	"github.com/nimling/samna-auth-middleware/shared"
)

func GetNotifications(ctx echo.Context) error {
	userID, ok := ctx.Get("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return respondError(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	if strings.Contains(ctx.Request().Header.Get("Accept"), "text/event-stream") {
		return notification.Stream(ctx, userID)
	}
	items := make([]shared.Notification, 0)
	if err := sdb.Instance().NewSelect().
		Model(&items).
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		Order("sa_created_at DESC").
		Scan(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("GetNotifications: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respondJSON(ctx, http.StatusOK, items, len(items))
}

func PutNotificationRead(ctx echo.Context) error {
	return setNotificationTime(ctx, "read_at")
}

func PutNotificationClosed(ctx echo.Context) error {
	return setNotificationTime(ctx, "closed_at")
}

func DeleteNotification(ctx echo.Context) error {
	userID, ok := ctx.Get("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return respondError(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	res, err := sdb.Instance().NewUpdate().
		Model((*shared.Notification)(nil)).
		Set("sa_deleted_at = NOW()").
		Where("id = ?", id).
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("DeleteNotification: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return respondError(ctx, http.StatusNotFound, "Not found")
	}
	return respondJSON(ctx, http.StatusOK, map[string]string{"id": id.String()}, 1)
}

func setNotificationTime(ctx echo.Context, column string) error {
	userID, ok := ctx.Get("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return respondError(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respondError(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	var updated shared.Notification
	res, err := sdb.Instance().NewUpdate().
		Model(&updated).
		Set("? = NOW()", bun.Ident(column)).
		Where("id = ?", id).
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		Returning("*").
		Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("setNotificationTime %s: %v", column, err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return respondError(ctx, http.StatusNotFound, "Not found")
	}
	return respondJSON(ctx, http.StatusOK, updated, 1)
}
