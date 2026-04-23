package utils

import (
	"net/http"
	"os"
	"strings"

	"github.com/labstack/echo/v4"
)

func WriteCookie(ctx echo.Context, cookie *http.Cookie) {
	domain := os.Getenv("SAUTH_COOKIE_DOMAIN")
	host := strings.Split(domain, ":")[0]
	secure := true

	if host == "localhost" {
		domain = ""
		secure = false
	} else {
		domain = host
	}

	cookie = &http.Cookie{
		Name:     cookie.Name,
		Value:    cookie.Value,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Path:     cookie.Path,
		MaxAge:   cookie.MaxAge,
		Domain:   domain,
	}

	ctx.SetCookie(cookie)
}

func WritePublicCookie(ctx echo.Context, cookie *http.Cookie) {
	domain := os.Getenv("SAUTH_COOKIE_DOMAIN")
	host := strings.Split(domain, ":")[0]
	secure := true

	if host == "localhost" {
		domain = ""
		secure = false
	}

	cookie = &http.Cookie{
		Name:     cookie.Name,
		Value:    cookie.Value,
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Path:     cookie.Path,
		MaxAge:   cookie.MaxAge,
		Domain:   domain,
	}

	ctx.SetCookie(cookie)
}

func ClearCookie(ctx echo.Context, name string) {
	domain := os.Getenv("SAUTH_COOKIE_DOMAIN")
	host := strings.Split(domain, ":")[0]
	secure := true

	if host == "localhost" {
		domain = ""
		secure = false
	} else {
		domain = host
	}

	ctx.SetCookie(&http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		Domain:   domain,
	})
}
