package sauth

import (
	"github.com/labstack/echo/v4"
	"net/http"
	"net/url"
)

func GetLoginUrl(redirectString *string) func(ctx echo.Context) error {
	redirectURI := ""
	if redirectString != nil {
		redirectURI = "?redirect_uri=" + url.QueryEscape(*redirectString)
	}
	return func(ctx echo.Context) error {
		return ctx.String(http.StatusOK, getEnvURL("SAMNA_AUTH_SERVER").String()+redirectURI)
	}
}
