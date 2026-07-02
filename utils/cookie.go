package utils

import (
	"net/http"
	"os"
	"strings"

	"github.com/labstack/echo/v4"
)

func cookieSameSite() http.SameSite {
	switch strings.ToLower(os.Getenv("SAUTH_COOKIE_SAMESITE")) {
	case "none":
		return http.SameSiteNoneMode
	case "strict":
		return http.SameSiteStrictMode
	default:
		return http.SameSiteLaxMode
	}
}

func cookieDomain() (string, bool) {
	domain := os.Getenv("SAUTH_COOKIE_DOMAIN")
	if domain == "" {
		domain = CookieDomainFromURI(os.Getenv("SAUTH_REDIRECT_URI"))
	}
	secure := domain != ""
	if v := os.Getenv("SAUTH_COOKIE_SECURE"); v != "" {
		secure = v == "true"
	}
	if cookieSameSite() == http.SameSiteNoneMode {
		secure = true
	}
	return domain, secure
}

func WriteCookie(ctx echo.Context, cookie *http.Cookie) {
	domain, secure := cookieDomain()

	cookie = &http.Cookie{
		Name:     cookie.Name,
		Value:    cookie.Value,
		HttpOnly: true,
		Secure:   secure,
		SameSite: cookieSameSite(),
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
		SameSite: cookieSameSite(),
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
		SameSite: cookieSameSite(),
		Domain:   domain,
	})
}
