package sauth

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

const UserIDKey = "user_id"

const authCodeParamKey = "sauth_auth_code"

type AuthMiddlewareConfig struct {
	ClientID            uuid.UUID
	ClientSlug          string
	ClientSecret        string
	TokenURL            url.URL
	JWKSURL             url.URL
	ExpectedTokenIssuer string
	CookieDomain        string
	RedirectURI         string

	// No auth for easy development/testing
	// TODO:: remove in future versions
	AuthorizedUserID *uuid.UUID
}

// AuthMiddlewareEnv returns a auth middleware configured from env
func AuthMiddlewareEnv() echo.MiddlewareFunc {
	return AuthMiddleware(nil)
}

// AuthMiddleware returns a middleware that validates requests against the Samna Auth
func AuthMiddleware(config *AuthMiddlewareConfig) echo.MiddlewareFunc {
	if config == nil {
		config = configFromEnv()
	}

	// No auth for easy development/testing
	// TODO:: remove in future versions
	if config.AuthorizedUserID != nil {
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(ctx echo.Context) error {
				ctx.Set(UserIDKey, *config.AuthorizedUserID)
				return next(ctx)
			}
		}
	}

	jwksClient, err := keyfunc.NewDefault([]string{config.JWKSURL.String()})
	if err != nil {
		panic(fmt.Sprintf("JWK client initialization failed: %v", err))
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {

			accessToken, err := getAccessToken(ctx)
			if err == nil { // If access token is present in header/cookie
				claimUser, vldErr := ValidateToken(accessToken, config.ExpectedTokenIssuer, jwksClient.Keyfunc)
				if vldErr == nil { // If access token is valid
					ctx.Set(UserIDKey, claimUser.ID)
					return next(ctx)
				}

				if !errors.Is(vldErr, ErrorTokenExpired) { // If token is invalid/malformed
					ctx.Logger().Errorf("Failed to validate the access_token: %v", vldErr)
					return ctx.String(http.StatusUnauthorized, "Unauthorized")
				}
			}

			if err != nil { // If error obtaining token from header/cookie
				// There are no server error, most likely the browser sent invalid cookie, or there is no cookie
				// at all. In any case we are moving on to the refresh or auth code processing logic
				ctx.Logger().Infof("Failed to retrieve the access_token: %v", err)
			}

			// If authCode is set we do token request, set tokens in cookie and redirect to the original request URL
			if ctx.QueryParams().Has(authCodeParamKey) {
				authCode := ctx.QueryParam(authCodeParamKey)

				err = exchangeAuthorizationCode(ctx, authCode, *config)
				if err != nil {
					ctx.Logger().Warnf("Failed to exchange the authorization code: %v", err)
					if errors.Is(err, ErrorUnauthorized) {
						return ctx.String(http.StatusUnauthorized, "Unauthorized")
					}

					return ctx.String(http.StatusInternalServerError, "Internal Server Error")
				}

				return ctx.Redirect(http.StatusFound, ctx.Path())
			}

			// Refresh tokens, set them in cookie
			tokens, err := refreshTokens(ctx, *config)
			if err != nil {
				ctx.Logger().Warnf("Failed to refresh tokens: %v", err)
				if errors.Is(err, ErrorUnauthorized) {
					return ctx.String(http.StatusUnauthorized, "Unauthorized")
				}

				return ctx.String(http.StatusInternalServerError, "Internal Server Error")
			}

			// No need to validate token which was just obtained from trusted auth server
			claims, err := claimsFromTokenUnsafe(tokens.AccessToken)
			if err != nil {
				ctx.Logger().Errorf("Failed to parse the token claims: %v", err)
				return ctx.String(http.StatusInternalServerError, "Internal Server Error")
			}

			ctx.Set(UserIDKey, claims.ID)

			return next(ctx)
		}
	}
}

func configFromEnv() *AuthMiddlewareConfig {
	return &AuthMiddlewareConfig{
		ClientID:            getEnvUUID("SAMNA_AUTH_CLIENT_ID"),
		ClientSlug:          getEnv("SAMNA_AUTH_CLIENT_SLUG"),
		ClientSecret:        getEnv("SAMNA_AUTH_CLIENT_SECRET"),
		TokenURL:            getEnvURL("SAMNA_AUTH_TOKEN_URL"),
		JWKSURL:             getEnvURL("SAMNA_AUTH_JWKS_URL"),
		ExpectedTokenIssuer: getEnv("SAMNA_AUTH_EXPECTED_TOKEN_ISSUER"),
		CookieDomain:        getEnv("SAMNA_AUTH_COOKIE_DOMAIN"),
		RedirectURI:         getEnv("SAMNA_AUTH_CLIENT_REDIRECT_URI"),
		AuthorizedUserID:    getEnvUUIDOptional("SAMNA_AUTH_DEBUG_USER_ID"),
	}
}
