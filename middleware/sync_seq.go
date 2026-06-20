package middleware

import (
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/shared"
)

func SyncSeq(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		var seq int64
		err := shared.Instance().NewSelect().
			TableExpr("claimius.sync_event").
			ColumnExpr("COALESCE(MAX(seq), 0)").
			Scan(ctx.Request().Context(), &seq)
		if err == nil {
			ctx.Response().Header().Set("SAuth-Seq", strconv.FormatInt(seq, 10))
		}
		return next(ctx)
	}
}
