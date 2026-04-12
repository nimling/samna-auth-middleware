package middleware

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/db"
	"golang.org/x/crypto/bcrypt"
)

func ClientAuthMiddleware(opts *ClientAuthOptions) echo.MiddlewareFunc {
	if opts == nil {
		opts = &ClientAuthOptions{}
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			clientKeyHeader := ctx.Request().Header.Get("SAuth-Client-Key")
			clientSecretHeader := ctx.Request().Header.Get("SAuth-Client-Secret")

			if clientKeyHeader == "" || clientSecretHeader == "" {
				return next(ctx)
			}

			if ctx.Request().Header.Get(echo.HeaderAuthorization) != "" {
				return next(ctx)
			}

			clientKey, err := uuid.Parse(clientKeyHeader)
			if err != nil {
				return ctx.String(http.StatusUnauthorized, "Unauthorized")
			}

			samnaClient, err := db.Instance().GetClientByKey(ctx.Request().Context(), clientKey)
			if err != nil {
				return ctx.String(http.StatusUnauthorized, "Unauthorized")
			}

			if err := bcrypt.CompareHashAndPassword([]byte(samnaClient.SecretHash), []byte(clientSecretHeader)); err != nil {
				return ctx.String(http.StatusUnauthorized, "Unauthorized")
			}

			ctx.Set(UserIDKey, samnaClient.ID)

			return next(ctx)
		}
	}
}
