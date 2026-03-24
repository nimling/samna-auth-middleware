package echo

import (
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
)

func metaFromContext(ctx echo.Context) client.RequestMetadata {
	accessToken, _ := ctx.Get("access_token").(string)
	return client.RequestMetadata{
		AccessToken: accessToken,
		RequestID:   ctx.Response().Header().Get(echo.HeaderXRequestID),
		UserIP:      ctx.RealIP(),
		QueryParams: ctx.QueryParams(),
		DryRun:      ctx.Request().Header.Get("X-Is-Dry-Run") == "true",
	}
}
