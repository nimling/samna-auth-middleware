package smw

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
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
