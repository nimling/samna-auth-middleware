package smw

import (
	"database/sql"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/prophet/sdb"
)

// SyncSeq stamps, on a successful prophet answer to a write, the highest sync
// position of the calling app once the write has run. A disciple that routed
// the write waits until its cursor reaches it before it reads its own write.
func SyncSeq(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		switch ctx.Request().Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			return next(ctx)
		}
		res := ctx.Response()
		res.Before(func() {
			appID, _ := ctx.Get("app_id").(uuid.UUID)
			if appID == uuid.Nil || res.Status < 200 || res.Status >= 300 {
				return
			}
			var seq client.SyncCursor
			if err := sdb.Instance().NewRaw("SELECT txid, seq FROM claimius.get_sync_seq(?)", appID).
				Scan(ctx.Request().Context(), &seq); err != nil {
				ctx.Logger().Errorf("sync seq %s: %v", appID, err)
				return
			}
			res.Header().Set(client.HeaderSyncSeq, seq.String())
		})
		return next(ctx)
	}
}

// AppHome refuses a write for an app another prophet decides: an app mirrored
// from a peer, or one this prophet offered to a peer that has not yet taken
// it. The refusal answers 409 and names that prophet in client.HeaderHome, so
// every app keeps a single writer and a routed write learns where it belongs.
func AppHome(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		switch ctx.Request().Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			return next(ctx)
		}
		appID, _ := ctx.Get("app_id").(uuid.UUID)
		if appID == uuid.Nil {
			return next(ctx)
		}
		var home sql.NullString
		if err := sdb.Instance().NewRaw("SELECT claimius.get_app_home(?)", appID).Scan(ctx.Request().Context(), &home); err != nil {
			ctx.Logger().Errorf("app home %s: %v", appID, err)
			return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
		}
		if home.Valid {
			ctx.Response().Header().Set(client.HeaderHome, home.String)
			return respond.Error(ctx, http.StatusConflict, "The app's writes are decided by prophet "+home.String)
		}
		return next(ctx)
	}
}
