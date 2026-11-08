package smw

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

func UserAuthMiddleware(opts *UserAuthOptions) echo.MiddlewareFunc {
	config := configFromUserAuthOptions(opts)

	switch utils.GetDebugMode() {
	case utils.DebugModeUser:
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(ctx echo.Context) error {
				proceed := next
				ctx.Set(UserIDKey, *config.DebugUserID)
				if forwardErr := forwardUserToken(ctx); forwardErr != nil {
					return respondAuthError(ctx, forwardErr)
				}
				if forwardErr := forwardUserExternal(ctx); forwardErr != nil {
					return respondAuthError(ctx, forwardErr)
				}
				if value, source := readCode(ctx); value != "" {
					return serveCode(ctx, config, value, source, true, proceed)
				}
				return proceed(ctx)
			}
		}
	case utils.DebugModeClients:
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(ctx echo.Context) error {
				proceed := next
				clientErr := authenticateClient(ctx, config.DebugClients)
				if errors.Is(clientErr, ErrorCredentialMissing) {
					if value, source := readCode(ctx); value != "" {
						return serveCode(ctx, config, value, source, false, proceed)
					}
					if config.Guest {
						return serveAnonymous(ctx, proceed)
					}
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
				if value, source := readCode(ctx); value != "" {
					return serveCode(ctx, config, value, source, true, proceed)
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

	wait := WaitForSeq(config.SeqTimeout)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		proceed := wait(next)
		return func(ctx echo.Context) error {
			authenticated, authErr := authenticateRequest(ctx, config, jwksClient.Keyfunc)
			if authErr != nil {
				return respondAuthError(ctx, authErr)
			}
			if value, source := readCode(ctx); value != "" {
				return serveCode(ctx, config, value, source, authenticated, proceed)
			}
			return serveRequest(ctx, config, authenticated, proceed)
		}
	}
}

func authenticateRequest(ctx echo.Context, config *AuthMiddlewareConfig, keyFunc jwt.Keyfunc) (bool, error) {
	if bearerErr := authenticateBearer(ctx, config, keyFunc); !errors.Is(bearerErr, ErrorCredentialMissing) {
		return bearerErr == nil, bearerErr
	}
	if cookieErr := authenticateCookie(ctx, config, keyFunc); !errors.Is(cookieErr, ErrorCredentialMissing) {
		return cookieErr == nil, cookieErr
	}
	if clientErr := authenticateClient(ctx, nil); !errors.Is(clientErr, ErrorCredentialMissing) {
		if clientErr != nil {
			return false, clientErr
		}
		if forwardErr := forwardUserToken(ctx); forwardErr != nil {
			return false, forwardErr
		}
		if forwardErr := forwardUserExternal(ctx); forwardErr != nil {
			return false, forwardErr
		}
		return true, nil
	}
	return false, nil
}

func getLoginPath() string {

	redirect, err := url.Parse(utils.Cfg().RedirectURI)
	if err != nil || redirect.Path == "" {
		return ""
	}
	return path.Join(path.Dir(redirect.Path), "login")
}

func respondUnauthenticated(ctx echo.Context) error {
	if login := getLoginPath(); login != "" && isNavigation(ctx) {
		return ctx.Redirect(http.StatusFound, login)
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
		Guest:           opts.Guest,
		SeqTimeout:      opts.SeqTimeout,
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
