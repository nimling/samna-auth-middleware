package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/utils"
)

func AuthMiddlewareEnv() echo.MiddlewareFunc {
	return AuthMiddleware(nil)
}

func AuthMiddleware(config *AuthMiddlewareConfig) echo.MiddlewareFunc {
	if config == nil {
		config = configFromEnv()
	}

	if config.AuthorizedUserID != nil {
		return func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(ctx echo.Context) error {
				ctx.Set(UserIDKey, *config.AuthorizedUserID)
				return next(ctx)
			}
		}
	}

	if err := getOpenIDConfiguration(context.Background(), config); err != nil {
		panic(fmt.Errorf("getting OpenID configuration failed: %v", err))
	}

	jwksClient, err := keyfunc.NewDefault([]string{config.JWKSURL.String()})
	if err != nil {
		panic(fmt.Sprintf("JWK client initialization failed: %v", err))
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {

			accessToken, err := getAccessToken(ctx)
			if err == nil {
				claimUser, vldErr := ValidateToken(accessToken, config.ExpectedTokenIssuer, jwksClient.Keyfunc)
				if vldErr == nil {
					ctx.Set(UserIDKey, claimUser.ID)
					ctx.Set(AccessTokenKey, accessToken)
					return next(ctx)
				}

				if !errors.Is(vldErr, ErrorTokenExpired) {
					ctx.Logger().Errorf("Failed to validate the access_token: %v", vldErr)
					return ctx.String(http.StatusUnauthorized, "Unauthorized")
				}
			}

			if err != nil {
				ctx.Logger().Infof("Failed to retrieve the access_token: %v", err)
			}

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

			tokens, err := refreshTokens(ctx, *config)
			if err != nil {
				ctx.Logger().Warnf("Failed to refresh tokens: %v", err)
				if errors.Is(err, ErrorUnauthorized) {
					return ctx.String(http.StatusUnauthorized, "Unauthorized")
				}

				return ctx.String(http.StatusInternalServerError, "Internal Server Error")
			}

			claims, err := claimsFromTokenUnsafe(tokens.AccessToken)
			if err != nil {
				ctx.Logger().Errorf("Failed to parse the token claims: %v", err)
				return ctx.String(http.StatusInternalServerError, "Internal Server Error")
			}

			ctx.Set(UserIDKey, claims.ID)
			ctx.Set(AccessTokenKey, tokens.AccessToken)

			return next(ctx)
		}
	}
}

func configFromEnv() *AuthMiddlewareConfig {
	openIDUrlStr := utils.GetEnv("SAUTH_ENDPOINT") + "/api/client/" + utils.GetEnv("SAUTH_CLIENT_SLUG") + "/.well-known/openid-configuration"
	openIDUrl, err := url.Parse(openIDUrlStr)
	if err != nil {
		panic(fmt.Errorf("failed to parse openid configuration URL: %v", err))
	}

	return &AuthMiddlewareConfig{
		ClientSlug:       utils.GetEnv("SAUTH_CLIENT_SLUG"),
		ClientSecret:     utils.GetEnv("SAUTH_CLIENT_SECRET"),
		RedirectURI:      utils.GetEnv("SAUTH_REDIRECT_URI"),
		OpenIDConfigURL:  *openIDUrl,
		AuthorizedUserID: utils.GetEnvUUIDOptional("SAUTH_DEBUG_USER_ID"),
	}
}
