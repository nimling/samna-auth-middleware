package smw

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

// UserAuthMiddleware authenticates browser and bearer users on consumer routes.
// It checks a bearer token, then a session cookie, then client credentials, and
// reads the resolved OIDC issuer, jwks and token endpoints from the config that
// sdb.Init loaded. When auth is disabled it serves an unavailable response.
//
// Optional admits a request that carries no credential at all and leaves the
// actor unset, so a route can serve public content and still recognize a signed
// in visitor. A credential that is present but invalid is still refused.
func UserAuthMiddleware(opts *UserAuthOptions) echo.MiddlewareFunc {
	config := configFromUserAuthOptions(opts)

	switch shared.GetDebugMode() {
	case shared.DebugModeUser:
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(ctx echo.Context) error {
				proceed := stamped(next)
				ctx.Set(UserIDKey, *config.DebugUserID)
				if forwardErr := forwardUserToken(ctx); forwardErr != nil {
					return respondAuthError(ctx, forwardErr)
				}
				if forwardErr := forwardUserExternal(ctx); forwardErr != nil {
					return respondAuthError(ctx, forwardErr)
				}
				return proceed(ctx)
			}
		}
	case shared.DebugModeClients:
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(ctx echo.Context) error {
				proceed := stamped(next)
				clientErr := authenticateClient(ctx, config.DebugClients)
				if errors.Is(clientErr, ErrorCredentialMissing) {
					if config.Optional {
						return proceed(ctx)
					}
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
				return proceed(ctx)
			}
		}
	}

	c := utils.Cfg()
	if c.AuthDisabled {
		if config.Optional {
			return passthroughMiddleware()
		}
		return inactiveAppMiddleware()
	}
	config.ExpectedTokenIssuer = c.Issuer
	config.JWKSURL = c.JWKSURL
	config.TokenURL = c.TokenURL

	jwksClient, err := keyfunc.NewDefault([]string{config.JWKSURL.String()})
	if err != nil {
		panic(fmt.Sprintf("JWK client initialization failed: %v", err))
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			proceed := stamped(next)
			if bearerErr := authenticateBearer(ctx, config, jwksClient.Keyfunc); !errors.Is(bearerErr, ErrorCredentialMissing) {
				if bearerErr != nil {
					return respondAuthError(ctx, bearerErr)
				}
				return proceed(ctx)
			}

			if cookieErr := authenticateCookie(ctx, config, jwksClient.Keyfunc); !errors.Is(cookieErr, ErrorCredentialMissing) {
				if cookieErr != nil {
					return respondAuthError(ctx, cookieErr)
				}
				return proceed(ctx)
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
				return proceed(ctx)
			}

			if config.Optional {
				return proceed(ctx)
			}
			return respondUnauthenticated(ctx)
		}
	}
}

const loginRedirectPath = "/api/auth/login"

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

func passthroughMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			return next(ctx)
		}
	}
}

func configFromUserAuthOptions(opts *UserAuthOptions) *AuthMiddlewareConfig {
	if opts == nil {
		opts = &UserAuthOptions{}
	}

	c := utils.Cfg()

	slug := opts.ClientSlug
	if slug == "" {
		slug = c.AppSlug
	}
	secret := opts.ClientSecret
	if secret == "" {
		secret = c.AppSecret
	}
	redirectURI := opts.RedirectURI
	if redirectURI == "" {
		redirectURI = c.RedirectURI
	}
	pageRedirectURI := opts.PageRedirectURI
	if pageRedirectURI == "" {
		pageRedirectURI = c.PageRedirect
	}

	return &AuthMiddlewareConfig{
		ClientSlug:      slug,
		ClientSecret:    secret,
		RedirectURI:     redirectURI,
		PageRedirectURI: pageRedirectURI,
		DebugUserID:     parseUUID(c.DebugUserID),
		DebugClients:    parseDebugClients(),
		Optional:        opts.Optional,
	}
}

func parseDebugClients() map[string]DebugClient {
	raw := utils.Cfg().DebugClients
	if raw == "" {
		return nil
	}
	var clients map[string]DebugClient
	if err := json.Unmarshal([]byte(raw), &clients); err != nil {
		panic(fmt.Sprintf("Environment variable SAUTH_DEBUG_CLIENTS is not valid JSON: %v", err))
	}
	return clients
}

func parseUUID(val string) *uuid.UUID {
	if val == "" {
		return nil
	}
	id, err := uuid.Parse(val)
	if err != nil {
		panic("Environment variable SAUTH_DEBUG_USER_ID is not a valid UUID")
	}
	return &id
}
