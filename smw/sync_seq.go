package smw

import (
	"strconv"

	"github.com/labstack/echo/v4"
	sdb "github.com/nimling/samna-auth-middleware/prophet/sdb"
)

// SyncSeq stamps the current sync sequence header on prophet responses so a
// disciple can tell how far ahead the prophet is.
func SyncSeq(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		var seq int64
		err := sdb.Instance().NewSelect().
			TableExpr("claimius.sync_event").
			ColumnExpr("COALESCE(MAX(seq), 0)").
			Scan(ctx.Request().Context(), &seq)
		if err == nil {
			ctx.Response().Header().Set("SAuth-Seq", strconv.FormatInt(seq, 10))
		}
		return next(ctx)
	}
}
