package middleware

import (
	"github.com/google/uuid"
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

func GetLogout(ctx echo.Context) error {

	clientIDStr, ok := os.LookupEnv("SAMNA_AUTH_CLIENT_ID")
	if !ok {
		ctx.Logger().Error("Failed to locate SAMNA_AUTH_CLIENT_ID in environment")
		return ctx.NoContent(http.StatusInternalServerError)
	}

	clientID, err := uuid.Parse(clientIDStr)
	if err != nil {
		ctx.Logger().Error("The clientID sat from env via SAMNA_AUTH_CLIENT_ID is malformed")
		return ctx.NoContent(http.StatusInternalServerError)
	}

	clientSecret, ok := os.LookupEnv("SAMNA_AUTH_CLIENT_SECRET")
	if !ok {
		ctx.Logger().Error("No clientSecret specified for AuthMiddleware")
		return ctx.NoContent(http.StatusInternalServerError)
	}

	clientSlug, ok := os.LookupEnv("SAMNA_AUTH_CLIENT_SLUG")
	if !ok {
		ctx.Logger().Error("Invalid SAMNA_AUTH_CLIENT_SLUG")
	}

	authURL := getEnvURL("SAMNA_AUTH_SERVER")
	authURL.Path = "/revoke/" + clientSlug

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
	req.Header.Set("Samna-Client-Id", clientID.String())
	req.Header.Set("Samna-Client-Secret", clientSecret)

	// Perform request
	resp, err := client.Do(req)
	if err != nil {
		ctx.Logger().Infof("Failed to perform request : %s", err) //TODO:: Encrypt this a bit more?
		return ctx.NoContent(http.StatusInternalServerError)
	}

	if resp.StatusCode != http.StatusOK {
		ctx.Logger().Infof("Unauthorized: %d", resp.StatusCode) //TODO:: Encrypt this a bit more?
		return ctx.NoContent(http.StatusInternalServerError)
	}

	return ctx.NoContent(http.StatusOK)
}
