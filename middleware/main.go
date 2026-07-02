package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/shared"
	"github.com/nimling/samna-auth-middleware/utils"
)

func UserAuthMiddleware(opts *UserAuthOptions) echo.MiddlewareFunc {
	config := configFromUserAuthOptions(opts)

	switch shared.GetDebugMode() {
	case shared.DebugModeUser:
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(ctx echo.Context) error {
				ctx.Set(UserIDKey, *config.DebugUserID)
				if forwardErr := forwardUserToken(ctx); forwardErr != nil {
					return respondAuthError(ctx, forwardErr)
				}
				if forwardErr := forwardUserExternal(ctx); forwardErr != nil {
					return respondAuthError(ctx, forwardErr)
				}
				return next(ctx)
			}
		}
	case shared.DebugModeClients:
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(ctx echo.Context) error {
				clientErr := authenticateClient(ctx, config.DebugClients)
				if errors.Is(clientErr, ErrorCredentialMissing) {
					return ctx.String(http.StatusUnauthorized, "Unauthorized")
				}
				if clientErr != nil {
					return respondAuthError(ctx, clientErr)
				}
				if forwardErr := forwardUserToken(ctx); forwardErr != nil {
					return respondAuthError(ctx, forwardErr)
				}
				if forwardErr := forwardUserExternal(ctx); forwardErr != nil {
					return respondAuthError(ctx, forwardErr)
				}
				return next(ctx)
			}
		}
	}

	if err := getOpenIDConfiguration(context.Background(), config); err != nil {
		if errors.Is(err, client.ErrNotFound) {
			log.Printf("samna_auth_middleware: openid-configuration unavailable for slug %q; auth disabled", config.ClientSlug)
			return inactiveAppMiddleware()
		}
		panic(fmt.Errorf("getting OpenID configuration failed: %v", err))
	}

	jwksClient, err := keyfunc.NewDefault([]string{config.JWKSURL.String()})
	if err != nil {
		panic(fmt.Sprintf("JWK client initialization failed: %v", err))
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			if bearerErr := authenticateBearer(ctx, config, jwksClient.Keyfunc); !errors.Is(bearerErr, ErrorCredentialMissing) {
				if bearerErr != nil {
					return respondAuthError(ctx, bearerErr)
				}
				return next(ctx)
			}

			if cookieErr := authenticateCookie(ctx, config, jwksClient.Keyfunc); !errors.Is(cookieErr, ErrorCredentialMissing) {
				if cookieErr != nil {
					return respondAuthError(ctx, cookieErr)
				}
				return next(ctx)
			}

			if clientErr := authenticateClient(ctx, nil); !errors.Is(clientErr, ErrorCredentialMissing) {
				if clientErr != nil {
					return respondAuthError(ctx, clientErr)
				}
				if forwardErr := forwardUserToken(ctx); forwardErr != nil {
					return respondAuthError(ctx, forwardErr)
				}
				if forwardErr := forwardUserExternal(ctx); forwardErr != nil {
					return respondAuthError(ctx, forwardErr)
				}
				return next(ctx)
			}

			return respondUnauthenticated(ctx)
		}
	}
}

const loginRedirectPath = "/api/sauth/login"

func respondUnauthenticated(ctx echo.Context) error {
	if isNavigation(ctx) {
		return ctx.Redirect(http.StatusFound, loginRedirectPath)
	}
	return ctx.String(http.StatusUnauthorized, "Unauthorized")
}

func isNavigation(ctx echo.Context) bool {
	if ctx.Request().Header.Get("Sec-Fetch-Mode") == "navigate" {
		return true
	}
	return strings.Contains(ctx.Request().Header.Get("Accept"), "text/html")
}

func respondAuthError(ctx echo.Context, err error) error {
	if errors.Is(err, ErrorUserTokenNotRecognized) {
		return ctx.String(http.StatusNotFound, string(ErrorUserTokenNotRecognized))
	}
	if errors.Is(err, ErrorUserExternalNotRecognized) {
		return ctx.String(http.StatusNotFound, string(ErrorUserExternalNotRecognized))
	}
	if errors.Is(err, ErrorForbidden) {
		return ctx.NoContent(http.StatusForbidden)
	}
	if errors.Is(err, ErrorUnauthorized) {
		return respondUnauthenticated(ctx)
	}
	ctx.Logger().Errorf("auth error: %v", err)
	return ctx.String(http.StatusInternalServerError, "Internal Server Error")
}

func inactiveAppMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			return ctx.String(http.StatusServiceUnavailable, "Auth service unavailable")
		}
	}
}

func configFromUserAuthOptions(opts *UserAuthOptions) *AuthMiddlewareConfig {
	if opts == nil {
		opts = &UserAuthOptions{}
	}

	debugUserID := getEnvUUID("SAUTH_DEBUG_USER_ID")
	debugClients := parseDebugClients()
	debugMode := shared.GetDebugMode()

	getEnvLenient := func(key string) string {
		if debugMode != shared.DebugModeNone {
			return os.Getenv(key)
		}
		return utils.GetEnv(key)
	}

	slug := opts.ClientSlug
	if slug == "" {
		slug = getEnvLenient("SAUTH_APP_SLUG")
	}
	secret := opts.ClientSecret
	if secret == "" {
		secret = getEnvLenient("SAUTH_APP_SECRET")
	}
	redirectURI := opts.RedirectURI
	if redirectURI == "" {
		redirectURI = getEnvLenient("SAUTH_REDIRECT_URI")
	}
	pageRedirectURI := opts.PageRedirectURI
	if pageRedirectURI == "" {
		pageRedirectURI = getEnvLenient("SAUTH_PAGE_REDIRECT")
	}

	openIDUrlStr := getEnvLenient("SAUTH_ENDPOINT") + "/api/app/" + slug + "/.well-known/openid-configuration"
	openIDUrl, err := url.Parse(openIDUrlStr)
	if err != nil {
		panic(fmt.Errorf("failed to parse openid configuration URL: %v", err))
	}

	return &AuthMiddlewareConfig{
		ClientSlug:      slug,
		ClientSecret:    secret,
		RedirectURI:     redirectURI,
		PageRedirectURI: pageRedirectURI,
		OpenIDConfigURL: *openIDUrl,
		DebugUserID:     debugUserID,
		DebugClients:    debugClients,
	}
}

func parseDebugClients() map[string]DebugClient {
	raw := os.Getenv("SAUTH_DEBUG_CLIENTS")
	if raw == "" {
		return nil
	}
	var clients map[string]DebugClient
	if err := json.Unmarshal([]byte(raw), &clients); err != nil {
		panic(fmt.Sprintf("Environment variable SAUTH_DEBUG_CLIENTS is not valid JSON: %v", err))
	}
	return clients
}

func getEnvUUID(envKey string) *uuid.UUID {
	val := os.Getenv(envKey)
	if val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		panic(fmt.Sprintf("Environment variable %s is not a valid UUID", envKey))
	}
	return &id
}
