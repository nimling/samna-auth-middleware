package echo

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/uptrace/bun"

	"github.com/nimling/samna-auth-middleware/v2/notification"
	"github.com/nimling/samna-auth-middleware/v2/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
)

func GetNotifications(ctx echo.Context) error {
	userID, ok := ctx.Get("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	if strings.Contains(ctx.Request().Header.Get("Accept"), "text/event-stream") {
		return notification.Stream(ctx, userID)
	}
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	items := make([]shared.Notification, 0)
	q := sdb.Instance().NewSelect().
		Model(&items).
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL").
		Order("sa_created_at DESC")
	if appID != uuid.Nil {
		q = q.Where("app_id = ?", appID)
	}
	if err := q.Scan(ctx.Request().Context()); err != nil {
		ctx.Logger().Errorf("GetNotifications: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	return respond.JSON(ctx, http.StatusOK, items, len(items))
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
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	q := sdb.Instance().NewUpdate().
		Model((*shared.Notification)(nil)).
		Set("sa_deleted_at = NOW()").
		Where("id = ?", id).
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL")
	if appID != uuid.Nil {
		q = q.Where("app_id = ?", appID)
	}
	res, err := q.Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("DeleteNotification: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return respond.Error(ctx, http.StatusNotFound, "Not found")
	}
	return respond.JSON(ctx, http.StatusOK, map[string]string{"id": id.String()}, 1)
}

func setNotificationTime(ctx echo.Context, column string) error {
	userID, ok := ctx.Get("user_id").(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return respond.Error(ctx, http.StatusUnauthorized, "Unauthorized")
	}
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "id must be a uuid")
	}
	appID, _ := ctx.Get("app_id").(uuid.UUID)
	var updated shared.Notification
	q := sdb.Instance().NewUpdate().
		Model(&updated).
		Set("? = NOW()", bun.Ident(column)).
		Where("id = ?", id).
		Where("user_id = ?", userID).
		Where("sa_deleted_at IS NULL")
	if appID != uuid.Nil {
		q = q.Where("app_id = ?", appID)
	}
	res, err := q.Returning("*").Exec(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("setNotificationTime %s: %v", column, err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return respond.Error(ctx, http.StatusNotFound, "Not found")
	}
	return respond.JSON(ctx, http.StatusOK, updated, 1)
}
