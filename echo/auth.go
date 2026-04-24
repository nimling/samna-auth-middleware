package echo

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/utils"
)

func GetLogin(ctx echo.Context) error {
	var provider *string
	if p := ctx.QueryParam("provider"); len(p) > 0 {
		provider = &p
	}

	redirectURL, err := client.GetLogin(ctx.Request().Context(), provider)
	if err != nil {
		ctx.Logger().Errorf("GetLogin: %v", err)
		return respondError(ctx, http.StatusForbidden, "Forbidden")
	}
	return Redirect(ctx, http.StatusTemporaryRedirect, redirectURL)
}

func GetLogout(ctx echo.Context) error {
	for _, name := range []string{"sauth_csrf_token", "sauth_refresh_token", "sauth_access_token"} {
		utils.ClearCookie(ctx, name)
	}

	statusCode, err := client.GetLogout(ctx.Request().Context())
	if err != nil {
		ctx.Logger().Errorf("GetLogout: %v", err)
		return ctx.NoContent(http.StatusInternalServerError)
	}
	return ctx.NoContent(statusCode)
}
