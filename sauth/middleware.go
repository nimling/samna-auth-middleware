package sauth

import (
	"errors"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// AuthMiddlewareEnv returns a auth middleware configured from env
func AuthMiddlewareEnv() echo.MiddlewareFunc {
	return AuthMiddleware(nil, nil, nil, nil)
}

// AuthMiddleware returns a middleware that validates requests against the Samna Auth
func AuthMiddleware(authURL *url.URL, clientSlug *string, clientID *uuid.UUID, clientSecret *string) echo.MiddlewareFunc {

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

	// TODO:: Redo all of this, /api should be included in the env url?
	authURL.Path = "/api/authorize/" + *clientSlug
	refreshURL := *authURL
	refreshURL.Path = "/api/token/" + *clientSlug

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

			// TODO:: Move this into the cooke check instead
			if ctx.QueryParams().Has("samna_auth_code") {
				// Get the auth code to exchange
				authCode := ctx.QueryParam("samna_auth_code")
				if len(authCode) > 0 {
					return exchangeAuthorizationCode(ctx, next, authCode, *authURL, *clientID, *clientSecret)
				}
			}

			accessToken, err := getAccessToken(ctx)
			if err == nil {
				ctx.Logger().Info("Retrieved access_token")
				claimUser, err := validateToken(ctx, *clientSecret, accessToken)
				if err == nil {
					ctx.Set("user_id", claimUser.ID) // TODO:: Excessive??
					ctx.Set("user", *claimUser)
					return next(ctx)
				}

				if !errors.Is(err, ErrorTokenExpired) {
					ctx.Logger().Infof("Failed to validate the access_token: %s", err)
					return ctx.String(http.StatusUnauthorized, "Unauthorized")
				}
			}

			if err != nil && !errors.Is(err, ErrorNoCookie) {
				ctx.Logger().Infof("Failed to retrieve the access_token: %s", err)
				return ctx.String(http.StatusInternalServerError, "Internal server error")
			}

			_, err = ctx.Cookie("refresh_token")
			if err != nil && errors.Is(err, http.ErrNoCookie) {
				ctx.Logger().Info("No refresh token found")
				return ctx.String(http.StatusUnauthorized, "Unauthorized")
			}

			// TODO:: Better safer way to set this?

			claimUser, err := refreshTokens(ctx, refreshURL, *clientID, *clientSecret)
			if err != nil {
				// TODO:: This might get a token expired error which is strange
				ctx.Logger().Infof("Failed to refresh tokens: %s", err)
				if errors.Is(err, ErrorUnauthorized) {
					return ctx.String(http.StatusUnauthorized, "Unauthorized")
				}
				return ctx.String(http.StatusInternalServerError, "Error refreshing the access token")
			}

			ctx.Set("user_id", claimUser.ID)
			ctx.Set("user", *claimUser)

			return next(ctx)
		}
	}
}

// getAccessToken retrieves the token either from Authorization header or cookie
func getAccessToken(ctx echo.Context) (string, error) {
	// Check if auth header is present
	authHeader := ctx.Request().Header.Get(echo.HeaderAuthorization)
	if len(authHeader) > 0 {
		if strings.HasPrefix(authHeader, "Bearer ") {
			return strings.TrimPrefix(authHeader, "Bearer "), nil
		}
	}

	// Get the access token from the cookie
	cookie, err := ctx.Cookie("access_token")
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			ctx.Logger().Infof("Missing access_token cookie: %s", err)
			return "", ErrorNoCookie
		}

		ctx.Logger().Infof("Failed to retrive the access_token: %s", err)
		//TODO:: Remove all other code except unauthorized? THis could also be expired
		return "", err
	}

	return cookie.Value, nil
}
