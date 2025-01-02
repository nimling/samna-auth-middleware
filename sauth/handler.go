package sauth

import (
	"github.com/labstack/echo/v4"
	"net/http"
	"os"
)

func RedirectLogin(ctx echo.Context) error {
	clientSlug, ok := os.LookupEnv("SAMNA_AUTH_CLIENT_SLUG")
	if !ok {
		panic("Invalid SAMNA_AUTH_CLIENT_SLUG")
	}

	authURL := getEnvURL("SAMNA_AUTH_SERVER")
	authURL.Path = "/login/" + clientSlug

	return ctx.Redirect(http.StatusTemporaryRedirect, authURL.String())
}

func RedirectLogout(ctx echo.Context) error {
	clientSlug, ok := os.LookupEnv("SAMNA_AUTH_CLIENT_SLUG")
	if !ok {
		panic("Invalid SAMNA_AUTH_CLIENT_SLUG")
	}

	authURL := getEnvURL("SAMNA_AUTH_SERVER")
	authURL.Path = "/revoke/" + clientSlug

	return ctx.Redirect(http.StatusTemporaryRedirect, authURL.String())
}
