package echo

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/sync"
)

func PostSync(ctx echo.Context) error {
	slug := ctx.Request().Header.Get("SAuth-App-Slug")
	secret := ctx.Request().Header.Get("SAuth-App-Secret")
	if !client.VerifyRequest(slug, secret) {
		return ctx.String(http.StatusUnauthorized, "Unauthorized")
	}

	var payload sync.SyncPayload
	if err := ctx.Bind(&payload); err != nil {
		return ctx.String(http.StatusBadRequest, "Bad Request")
	}

	if err := sync.ApplyPayload(ctx.Request().Context(), payload); err != nil {
		ctx.Logger().Errorf("sync apply %s: %v", payload.EventType, err)
		return ctx.String(http.StatusInternalServerError, "Internal Server Error")
	}

	if payload.Seq > 0 {
		if err := sync.UpdateSeq(ctx.Request().Context(), payload.Seq); err != nil {
			ctx.Logger().Errorf("sync update seq: %v", err)
		}
	}

	return ctx.String(http.StatusOK, "OK")
}

func GetSyncPing(ctx echo.Context) error {
	slug := ctx.Request().Header.Get("SAuth-App-Slug")
	secret := ctx.Request().Header.Get("SAuth-App-Secret")
	if !client.VerifyRequest(slug, secret) {
		return ctx.String(http.StatusUnauthorized, "Unauthorized")
	}
	return ctx.String(http.StatusOK, "OK")
}
