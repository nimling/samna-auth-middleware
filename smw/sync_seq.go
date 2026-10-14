package smw

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	discipledb "github.com/nimling/samna-auth-middleware/v2/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/v2/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
)

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

const seqTimeout = 10 * time.Second

func WaitForSeq(timeout time.Duration) echo.MiddlewareFunc {
	if timeout <= 0 {
		timeout = seqTimeout
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			db := discipledb.Instance()
			if db == nil || !db.RouteToProphet {
				return next(ctx)
			}
			reqCtx, seq, fresh := client.WithRequestSeq(ctx.Request().Context())
			if !fresh {
				return next(ctx)
			}
			ctx.SetRequest(ctx.Request().WithContext(reqCtx))
			ctx.Response().Before(func() {
				target := seq.Get()
				if target.IsZero() {
					return
				}
				if err := db.WaitForSeq(ctx.Request().Context(), target, timeout); err != nil && !errors.Is(err, sql.ErrNoRows) {
					ctx.Logger().Warnf("routed write %s not yet applied, answering anyway: %v", target, err)
				}
			})
			return next(ctx)
		}
	}
}
