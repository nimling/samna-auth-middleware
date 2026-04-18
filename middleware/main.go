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

func Auth(opts *AuthOptions) echo.MiddlewareFunc {
	if opts == nil {
		opts = &AuthOptions{}
	}

	var userMW echo.MiddlewareFunc
	var clientMW echo.MiddlewareFunc

	if opts.UserAuth != nil {
		userMW = UserAuthMiddleware(opts.UserAuth)
	} else {
		userMW = UserAuthMiddleware(nil)
	}

	if opts.ClientAuth != nil {
		clientMW = ClientAuthMiddleware(opts.ClientAuth)
	} else {
		clientMW = ClientAuthMiddleware(nil)
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			withClient := clientMW(next)
			withUser := userMW(withClient)
			return withUser(ctx)
		}
	}
}

func AuthMiddlewareEnv() echo.MiddlewareFunc {
	return AuthMiddleware(nil)
}

func AuthMiddleware(config *AuthMiddlewareConfig) echo.MiddlewareFunc {
	return UserAuthMiddleware(userAuthOptionsFromConfig(config))
}

func UserAuthMiddleware(opts *UserAuthOptions) echo.MiddlewareFunc {
	config := configFromUserAuthOptions(opts)

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

func userAuthOptionsFromConfig(config *AuthMiddlewareConfig) *UserAuthOptions {
	if config == nil {
		return nil
	}
	return &UserAuthOptions{
		ClientSlug:   config.ClientSlug,
		ClientSecret: config.ClientSecret,
		RedirectURI:  config.RedirectURI,
		DebugUserID:  config.AuthorizedUserID,
	}
}

func configFromUserAuthOptions(opts *UserAuthOptions) *AuthMiddlewareConfig {
	if opts == nil {
		return configFromEnv()
	}

	slug := opts.ClientSlug
	if slug == "" {
		slug = utils.GetEnv("SAUTH_APP_SLUG")
	}
	secret := opts.ClientSecret
	if secret == "" {
		secret = utils.GetEnv("SAUTH_APP_SECRET")
	}
	redirectURI := opts.RedirectURI
	if redirectURI == "" {
		redirectURI = utils.GetEnv("SAUTH_REDIRECT_URI")
	}
	debugUserID := opts.DebugUserID
	if debugUserID == nil {
		debugUserID = utils.GetEnvUUIDOptional("SAUTH_DEBUG_USER_ID")
	}

	openIDUrlStr := utils.GetEnv("SAUTH_ENDPOINT") + "/api/app/" + slug + "/.well-known/openid-configuration"
	openIDUrl, err := url.Parse(openIDUrlStr)
	if err != nil {
		panic(fmt.Errorf("failed to parse openid configuration URL: %v", err))
	}

	return &AuthMiddlewareConfig{
		ClientSlug:       slug,
		ClientSecret:     secret,
		RedirectURI:      redirectURI,
		OpenIDConfigURL:  *openIDUrl,
		AuthorizedUserID: debugUserID,
	}
}

func configFromEnv() *AuthMiddlewareConfig {
	openIDUrlStr := utils.GetEnv("SAUTH_ENDPOINT") + "/api/app/" + utils.GetEnv("SAUTH_APP_SLUG") + "/.well-known/openid-configuration"
	openIDUrl, err := url.Parse(openIDUrlStr)
	if err != nil {
		panic(fmt.Errorf("failed to parse openid configuration URL: %v", err))
	}

	return &AuthMiddlewareConfig{
		ClientSlug:       utils.GetEnv("SAUTH_APP_SLUG"),
		ClientSecret:     utils.GetEnv("SAUTH_APP_SECRET"),
		RedirectURI:      utils.GetEnv("SAUTH_REDIRECT_URI"),
		OpenIDConfigURL:  *openIDUrl,
		AuthorizedUserID: utils.GetEnvUUIDOptional("SAUTH_DEBUG_USER_ID"),
	}
}
