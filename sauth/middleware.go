package sauth

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"net/http"
	"net/url"
	"os"
)

func AuthMiddlewareEnv() echo.MiddlewareFunc {
	return AuthMiddleware(nil, nil, nil, nil, nil)
}

// AuthMiddleware returns a middleware that validates requests against the Samna Auth
func AuthMiddleware(cookieDomain *url.URL, authURL *url.URL, clientSlug *string, clientID *uuid.UUID, clientSecret *string) echo.MiddlewareFunc {

	if cookieDomain == nil {
		cookieDomain = getEnvURL("SAMNA_AUTH_DOMAIN")
	}

	if authURL == nil {
		authURL = getEnvURL("SAMNA_AUTH_SERVER")
	}

	if clientSlug == nil {
		envVal, ok := os.LookupEnv("SAMNA_AUTH_CLIENT_SLUG")
		if !ok {
			panic("No clientID specified for AuthMiddleware")
		}
		clientSlug = &envVal
	}

	// TODO:: Redo all of this
	authURL.Path = "/auth/validate/" + *clientSlug

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

	// TODO:: Better initialization and env paramter validation

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {

			if ctx.QueryParams().Has("samna_auth_code") {
				// Get the auth code to exchange
				authCode := ctx.QueryParam("samna_auth_code")
				if len(authCode) > 0 {
					return ExchangeAuthorizationCode(ctx, authCode, *cookieDomain, *authURL, *clientID, *clientSecret)
				}
			}

			// Get the access token from the cookie
			cookie, err := ctx.Cookie("access_token")
			if err != nil {
				if errors.Is(err, http.ErrNoCookie) {
					ctx.Logger().Infof("Missing access_token cookie: %s", err)
					return ctx.String(http.StatusUnauthorized, "Unauthorized")
				}

				ctx.Logger().Infof("Failed to retrive the access_token: %s", err)
				//TODO:: Remove all other code except unauthorized? THis could also be expired
				return ctx.String(http.StatusInternalServerError, "Error retrieving access token")
			}

			ctx.Logger().Infof("Retrieved access_token: %s", cookie.Value)
			return validateToken(ctx, next, *clientSecret, cookie.Value)
		}
	}
}

// ExchangeAuthorizationCode redirects the request to the auth middleware
func ExchangeAuthorizationCode(ctx echo.Context, authCode string, cookieDomain url.URL, authURL url.URL, clientID uuid.UUID, clientSecret string) error {
	bodyCode := BodyCode{
		Code: &authCode,
	}

	jsonBody, err := json.Marshal(bodyCode)
	if err != nil {
		ctx.Logger().Error("Malformed validation body")
		return ctx.String(http.StatusInternalServerError, "Malformed validation body")
	}

	// Verify with auth service
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodPost, authURL.String(), bytes.NewBuffer(jsonBody))
	if err != nil {
		ctx.Logger().Error("Failed to create a request to redirect login URL") //TODO:: Encrypt this a bit more?
		return ctx.String(http.StatusInternalServerError, "Failed to create a request to redirect login URL")
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Samna-Client-Id", clientID.String())
	req.Header.Set("Samna-Client-Secret", clientSecret)

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

	return ctx.NoContent(http.StatusOK) //TODO:: This should be redirected to the original request performing the call
}
