package utils

import (
	"net/http"
	"os"
	"strings"

	"github.com/labstack/echo/v4"
)

func WriteCookie(ctx echo.Context, cookie *http.Cookie) {
	cookie = &http.Cookie{
		Name:     cookie.Name,
		Value:    cookie.Value,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Path:     cookie.Path,
		MaxAge:   cookie.MaxAge,
	}

	cookieDomain := os.Getenv("SAMNA_AUTH_COOKIE_DOMAIN") // TODO:: WE need to streamline and slim this down, should be the domain of the redirect domain always
	if cookieDomain != "" {
		cookie.Domain = cookieDomain
		if strings.Contains(cookieDomain, "localhost") {
			cookie.Secure = false
		}
	}

	ctx.SetCookie(cookie)
}

func ClearCookie(ctx echo.Context, name string) {
	WriteCookie(ctx, &http.Cookie{
		Name:   name,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
}
