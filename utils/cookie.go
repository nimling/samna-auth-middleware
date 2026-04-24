package utils

import (
	"net/http"
	"os"

	"github.com/labstack/echo/v4"
)

func cookieDomain() (string, bool) {
	domain := CookieDomainFromURI(os.Getenv("SAUTH_REDIRECT_URI"))
	if domain == "" {
		return "", false
	}
	return domain, true
}

func WriteCookie(ctx echo.Context, cookie *http.Cookie) {
	domain, secure := cookieDomain()

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
	domain, secure := cookieDomain()

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
	domain, secure := cookieDomain()

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
