package sauth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"net/http"
	"net/url"
	"os"
)

func AuthMiddlewareFromEnv() echo.MiddlewareFunc {
	return AuthMiddleware(nil, nil, nil, nil)
}

// AuthMiddleware returns a middleware that validates requests against the Samna Auth
func AuthMiddleware(cookieDomain *url.URL, authURL *url.URL, clientID *uuid.UUID, clientSecret *string) echo.MiddlewareFunc {

	if cookieDomain == nil {
		cookieDomain = getEnvURL("SAMNA_AUTH_DOMAIN")
	}

	if authURL == nil {
		authURL = getEnvURL("SAMNA_AUTH_SERVER")
	}

	if clientID == nil {
		envVal, ok := os.LookupEnv("SAMNA_AUTH_CLIENT_ID")
		if !ok {
			panic("No clientID specified for AuthMiddleware")
		}

		parsedVal, err := uuid.Parse(envVal)
		if err != nil {
			panic("The clientID sat from env via SAMNA_AUTH_CLIENT_ID is malformed")
		}
		clientID = &parsedVal
	}

	if clientSecret == nil {
		envVal, ok := os.LookupEnv("SAMNA_AUTH_CLIENT_SECRET")
		if !ok {
			panic("No clientSecret specified for AuthMiddleware")
		}

		clientSecret = &envVal
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			// Get the access token from the cookie
			cookie, err := ctx.Cookie("access_token")
			if err != nil {
				if authURL == nil {
					ctx.Logger().Error("Invalid or missing auth URL")
					// TODO:: This might be more of a panic scenario?
					return ctx.String(http.StatusInternalServerError, "Invalid or missing auth URL")
				}

				if cookieDomain == nil {
					ctx.Logger().Error("Invalid or missing auth cookie domain")
					// TODO:: This might be more of a panic scenario?
					return ctx.String(http.StatusInternalServerError, "Invalid or missing auth cookie domain")
				}

				if clientID == nil {
					ctx.Logger().Error("Invalid or missing auth client ID")
					// TODO:: This might be more of a panic scenario?
					return ctx.String(http.StatusInternalServerError, "Invalid or missing auth client ID")
				}

				if clientSecret == nil {
					ctx.Logger().Error("Invalid or missing auth client secret")
					// TODO:: This might be more of a panic scenario?
					return ctx.String(http.StatusInternalServerError, "Invalid or missing auth client secret")
				}

				if errors.Is(err, http.ErrNoCookie) {
					ctx.Logger().Infof("Missing access_token cookie: %s", err)
					return RedirectLogin(ctx, *cookieDomain, *authURL, *clientID, *clientSecret) //TODO:: We need to check the authURL here as well
				}

				ctx.Logger().Infof("Failed to retrive the access_token: %s", err)
				//TODO:: Remove all other code except unauthorized? THis could also be expired
				return ctx.String(http.StatusInternalServerError, "Error retrieving access token")
			}

			ctx.Logger().Infof("Retrieved access_token: %s", cookie.Value)

			if clientSecret == nil || *clientSecret == "" {
				ctx.Logger().Error("Invalid client secret")
				// TODO:: This might be more of a panic scenario?
				return ctx.String(http.StatusInternalServerError, "Invalid or missing client secret")
			}

			return validateToken(ctx, next, *clientSecret, cookie.Value)
		}
	}
}

// RedirectLogin redirects the request to the auth middleware
func RedirectLogin(ctx echo.Context, cookieDomain url.URL, authURL url.URL, clientID uuid.UUID, clientSecret string) error {
	// Build redirect url
	redirectURI := fmt.Sprintf("%s://%s%s",
		ctx.Scheme(),
		ctx.Request().Host,
		ctx.Request().URL.String()) //TODO:: How to build this for the proper return?

	bodyData := BodyValidate{
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}

	jsonBody, err := json.Marshal(bodyData)
	if err != nil {
		ctx.Logger().Error("Malformed validation body")
		return ctx.String(http.StatusInternalServerError, "Malformed validation body")
	}

	// Verify with auth service
	client := &http.Client{}
	req, err := http.NewRequest("POST", authURL.String(), bytes.NewBuffer(jsonBody))
	if err != nil {
		ctx.Logger().Error("Failed to create a request to redirect login URL") //TODO:: Encrypt this a bit more?
		return ctx.String(http.StatusInternalServerError, "Failed to create a request to redirect login URL")
	}
	req.Header.Set("Content-Type", "application/json")

	// Perform request
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		ctx.Logger().Info("Unauthorized") //TODO:: Encrypt this a bit more?
		return ctx.String(http.StatusUnauthorized, "Unauthorized")
	}

	defer resp.Body.Close()

	// Parse response body
	var respBody ResponseToken
	if err = json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		ctx.Logger().Error("Failed to parse auth response body") //TODO:: Encrypt this a bit more?
		return ctx.String(http.StatusUnauthorized, "Invalid session")
	}

	// Write cookies
	err = writeCookie(ctx, &http.Cookie{
		Name:   "access_token",
		Value:  respBody.AccessToken,
		Domain: cookieDomain.String(),
		Path:   "/",
		MaxAge: 3600, // TODO:: We need to get this from the server
	})

	err = writeCookie(ctx, &http.Cookie{
		Name:   "refresh_token",
		Value:  respBody.RefreshToken,
		Domain: cookieDomain.String(),
		Path:   "/",     // TODO:: Set this to another specific path?
		MaxAge: 2592000, // TODO:: We need to get this from the server
	})

	return ctx.Redirect(http.StatusTemporaryRedirect, redirectURI)
}
