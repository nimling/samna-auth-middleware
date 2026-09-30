package echo

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/pkg/respond"
)

func GetSamApp(ctx echo.Context) error {
	app, ok := ctx.Get("app").(SamnaApp)
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "Missing app context")
	}

	app.PrivateKey = ""
	app.AppSecret = nil

	return respond.JSON(ctx, http.StatusOK, app, 1)
}
