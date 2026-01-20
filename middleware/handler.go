package middleware

import (
	"fmt"
	"net/http"
	"os"

	"github.com/labstack/echo/v4"
)

func RedirectLogin(ctx echo.Context) error {
	providerName := ctx.Param("provider")
	if len(providerName) == 0 {
		return echo.NewHTTPError(http.StatusForbidden, "Unknown provider")
	}

	clientSlug, ok := os.LookupEnv("SAMNA_AUTH_CLIENT_SLUG")
	if !ok {
		panic("Invalid SAMNA_AUTH_CLIENT_SLUG")
	}

	authURL := getEnvURL("SAMNA_AUTH_ENDPOINT")
	authURL.Path = fmt.Sprintf("/api/p/%s/login", providerName)

	q := authURL.Query()
	q.Set("client_slug", clientSlug)
	authURL.RawQuery = q.Encode()

	return ctx.Redirect(http.StatusTemporaryRedirect, authURL.String())
}

func GetLogout(ctx echo.Context) error {
	clientSecret, ok := os.LookupEnv("SAMNA_AUTH_CLIENT_SECRET")
	if !ok {
		ctx.Logger().Error("No clientSecret specified for AuthMiddleware")
		return ctx.NoContent(http.StatusInternalServerError)
	}

	clientSlug, ok := os.LookupEnv("SAMNA_AUTH_CLIENT_SLUG")
	if !ok {
		ctx.Logger().Error("Invalid SAMNA_AUTH_CLIENT_SLUG")
	}

	authURL := getEnvURL("SAMNA_AUTH_ENDPOINT")
	authURL.Path = "/api/logout"

	clearCookie(ctx, "sauth_csrf_token")
	clearCookie(ctx, "sauth_refresh_token")
	clearCookie(ctx, "sauth_access_token")

	// Verify with auth service
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, authURL.String(), nil)
	if err != nil {
		ctx.Logger().Error("Failed to create a request to redirect revoke URL") //TODO:: Encrypt this a bit more?
		return ctx.NoContent(http.StatusInternalServerError)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("SAuth-Client-Slug", clientSlug)
	req.Header.Set("SAuth-Client-Secret", clientSecret)
	req.Header.Set("Authorization", ctx.Request().Header.Get("Authorization"))

	if sessionCookie, err := ctx.Cookie("sauth_session"); err == nil {
		req.AddCookie(sessionCookie)
	}

	// Perform request
	resp, err := client.Do(req)
	if err != nil {
		ctx.Logger().Infof("Failed to perform request : %s", err) //TODO:: Encrypt this a bit more?
		return ctx.NoContent(http.StatusInternalServerError)
	}

	return ctx.NoContent(resp.StatusCode)
}
