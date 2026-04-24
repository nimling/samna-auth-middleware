package echo

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
)

func GetApp(ctx echo.Context) error {
	body, err := client.GetApp(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetApp: %v", err)
		return respondError(ctx, http.StatusInternalServerError, "Internal server error")
	}
	defer body.Close()
	return ctx.Stream(http.StatusOK, "application/json", body)
}
