package utils

import (
	"net/http"
	"os"
	"strings"

	"github.com/labstack/echo/v4"
)

const CookieScopeKey = "sauth_cookie_scope"

type CookieScope struct {
	Domain   string
	SameSite string
}

func getCookieScope(ctx echo.Context) (CookieScope, bool) {
	scope, ok := ctx.Get(CookieScopeKey).(CookieScope)
	return scope, ok
}

func cookieSameSite(ctx echo.Context) http.SameSite {
	sameSite := os.Getenv("SAUTH_COOKIE_SAMESITE")
	if scope, ok := getCookieScope(ctx); ok && scope.SameSite != "" {
		sameSite = scope.SameSite
	}
	switch strings.ToLower(sameSite) {
	case "none":
		return http.SameSiteNoneMode
	case "strict":
		return http.SameSiteStrictMode
	default:
		return http.SameSiteLaxMode
	}
}

func cookieDomain(ctx echo.Context) (string, bool) {
	domain := os.Getenv("SAUTH_COOKIE_DOMAIN")
	if domain == "" {
		domain = CookieDomainFromURI(Cfg().RedirectURI)
	}
	secure := domain != ""
	if scope, ok := getCookieScope(ctx); ok {
		domain = scope.Domain
		secure = true
	}
	if v := os.Getenv("SAUTH_COOKIE_SECURE"); v != "" {
		secure = v == "true"
	}
	if cookieSameSite(ctx) == http.SameSiteNoneMode {
		secure = true
	}
	return domain, secure
}

func WriteCookie(ctx echo.Context, cookie *http.Cookie) {
	domain, secure := cookieDomain(ctx)

	cookie = &http.Cookie{
		Name:     cookie.Name,
		Value:    cookie.Value,
		HttpOnly: true,
		Secure:   secure,
		SameSite: cookieSameSite(ctx),
		Path:     cookie.Path,
		MaxAge:   cookie.MaxAge,
		Domain:   domain,
	}

	ctx.SetCookie(cookie)
}

func WritePublicCookie(ctx echo.Context, cookie *http.Cookie) {
	domain, secure := cookieDomain(ctx)

	cookie = &http.Cookie{
		Name:     cookie.Name,
		Value:    cookie.Value,
		HttpOnly: false,
		Secure:   secure,
		SameSite: cookieSameSite(ctx),
		Path:     cookie.Path,
		MaxAge:   cookie.MaxAge,
		Domain:   domain,
	}

	ctx.SetCookie(cookie)
}

func ClearCookie(ctx echo.Context, name string) {
	domain, secure := cookieDomain(ctx)

	for _, scope := range clearDomains(ctx, domain) {
		ctx.SetCookie(&http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   secure,
			SameSite: cookieSameSite(ctx),
			Domain:   scope,
		})
	}
}

func clearDomains(ctx echo.Context, domain string) []string {
	domains := []string{domain}
	redirect := CookieDomainFromURI(Cfg().RedirectURI)
	if redirect != "" && redirect != domain {
		domains = append(domains, redirect)
	}
	if scope, ok := getCookieScope(ctx); ok && scope.Domain != "" && scope.Domain != domain && scope.Domain != redirect {
		domains = append(domains, scope.Domain)
	}
	return domains
}
