package echo

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/pkg/encryption"
	"github.com/nimling/samna-auth-middleware/v2/pkg/respond"
)

func GetSamApp(ctx echo.Context) error {
	app, ok := ctx.Get("app").(SamnaApp)
	if !ok {
		return respond.Error(ctx, http.StatusBadRequest, "Missing app context")
	}

	seed, err := encryption.UnwrapAtRest(app.PrivateSeed)
	if err != nil {
		ctx.Logger().Errorf("GetSamApp open seed of app %s: %v", app.Slug, err)
		return respond.Error(ctx, http.StatusInternalServerError, "App seed unavailable")
	}

	app.PrivateKey = ""
	app.PrivateSeed = seed
	app.AppSecret = nil

	ctx.Response().Header().Set(echo.HeaderCacheControl, "no-store")
	return respond.JSON(ctx, http.StatusOK, app, 1)
}
