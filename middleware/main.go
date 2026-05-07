package middleware

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/utils"
)

func UserAuthMiddleware(opts *UserAuthOptions) echo.MiddlewareFunc {
	config := configFromUserAuthOptions(opts)

	if config.AuthorizedUserID != nil {
		if err := ensureDebugUser(context.Background(), *config.AuthorizedUserID); err != nil {
			panic(fmt.Errorf("ensure debug user: %w", err))
		}
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(ctx echo.Context) error {
				ctx.Set(UserIDKey, *config.AuthorizedUserID)
				return next(ctx)
			}
		}
	}

	if db.IsDebugMode() {
		if config.DebugClientID != nil {
			if err := ensureDebugUser(context.Background(), *config.DebugClientID); err != nil {
				panic(fmt.Errorf("ensure debug user: %w", err))
			}
		}
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(ctx echo.Context) error {
				clientErr := authenticateClient(ctx, config.DebugClientID, config.DebugClientSecret)
				if errors.Is(clientErr, ErrorCredentialMissing) {
					return ctx.String(http.StatusUnauthorized, "Unauthorized")
				}
				if clientErr != nil {
					return respondAuthError(ctx, clientErr)
				}
				if forwardErr := forwardUserToken(ctx); forwardErr != nil {
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
			if handled, exErr := exchangeAuthCodeFromQuery(ctx, config); handled {
				if exErr != nil {
					ctx.Logger().Warnf("Failed to exchange the authorization code: %v", exErr)
					return respondAuthError(ctx, exErr)
				}
				return ctx.Redirect(http.StatusFound, config.PageRedirectURI)
			}

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

			if clientErr := authenticateClient(ctx, config.DebugClientID, config.DebugClientSecret); !errors.Is(clientErr, ErrorCredentialMissing) {
				if clientErr != nil {
					return respondAuthError(ctx, clientErr)
				}
				if forwardErr := forwardUserToken(ctx); forwardErr != nil {
					return respondAuthError(ctx, forwardErr)
				}
				return next(ctx)
			}

			return ctx.String(http.StatusUnauthorized, "Unauthorized")
		}
	}
}

func respondAuthError(ctx echo.Context, err error) error {
	if errors.Is(err, ErrorUnauthorized) {
		return ctx.String(http.StatusUnauthorized, "Unauthorized")
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

	debugUserID := opts.DebugUserID
	if debugUserID == nil {
		debugUserID = getEnvUUID("SAUTH_DEBUG_USER_ID")
	}
	debugClientID := getEnvUUID("SAUTH_DEBUG_CLIENT_ID")
	debugClientSecret := os.Getenv("SAUTH_DEBUG_CLIENT_SECRET")

	if debugUserID != nil && debugClientID != nil && *debugUserID != *debugClientID {
		panic("SAUTH_DEBUG_USER_ID and SAUTH_DEBUG_CLIENT_ID must hold the same UUID")
	}

	slug := opts.ClientSlug
	if slug == "" {
		slug = utils.GetEnv("SAUTH_APP_SLUG")
	}
	secret := opts.ClientSecret
	if secret == "" {
		if db.IsDebugMode() {
			secret = os.Getenv("SAUTH_APP_SECRET")
		} else {
			secret = utils.GetEnv("SAUTH_APP_SECRET")
		}
	}
	redirectURI := opts.RedirectURI
	if redirectURI == "" {
		redirectURI = utils.GetEnv("SAUTH_REDIRECT_URI")
	}
	pageRedirectURI := opts.PageRedirectURI
	if pageRedirectURI == "" {
		pageRedirectURI = utils.GetEnv("SAUTH_PAGE_REDIRECT")
	}

	openIDUrlStr := utils.GetEnv("SAUTH_ENDPOINT") + "/api/app/" + slug + "/.well-known/openid-configuration"
	openIDUrl, err := url.Parse(openIDUrlStr)
	if err != nil {
		panic(fmt.Errorf("failed to parse openid configuration URL: %v", err))
	}

	return &AuthMiddlewareConfig{
		ClientSlug:        slug,
		ClientSecret:      secret,
		RedirectURI:       redirectURI,
		PageRedirectURI:   pageRedirectURI,
		OpenIDConfigURL:   *openIDUrl,
		AuthorizedUserID:  debugUserID,
		DebugClientID:     debugClientID,
		DebugClientSecret: debugClientSecret,
	}
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
