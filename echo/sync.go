package echo

import (
	"encoding/hex"
	"net/http"
	"os"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/pkg/encryption"
	"github.com/nimling/samna-auth-middleware/shared"
	"github.com/nimling/samna-auth-middleware/sync"
)

func verifySync(ctx echo.Context) bool {
	slug := ctx.Request().Header.Get("SAuth-App-Slug")
	owner := ctx.Request().Header.Get("SAuth-App-Owner")
	secret := ctx.Request().Header.Get("SAuth-App-Secret")
	if slug == "" || secret == "" || owner == "" {
		return false
	}
	if slug != os.Getenv("SAUTH_APP_SLUG") {
		return false
	}
	ownerID, err := uuid.Parse(owner)
	if err != nil {
		return false
	}

	var org shared.Organization
	if err := shared.Instance().NewSelect().
		Model(&org).
		Column("private_seed").
		Where("id = ?", ownerID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx.Request().Context()); err != nil {
		return false
	}
	seed, err := hex.DecodeString(org.PrivateSeed)
	if err != nil {
		return false
	}
	return encryption.ValidateAppSecret(seed, os.Getenv("SAUTH_APP_SECRET"), secret)
}

func PostSync(ctx echo.Context) error {
	if !verifySync(ctx) {
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
	if !verifySync(ctx) {
		return ctx.String(http.StatusUnauthorized, "Unauthorized")
	}
	return ctx.String(http.StatusOK, "OK")
}
