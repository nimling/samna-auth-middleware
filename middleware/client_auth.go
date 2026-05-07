package middleware

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/utils"
	"golang.org/x/crypto/bcrypt"
)

const userTokenHeader = "SAuth-User-Token"

func ClientAuthMiddleware() echo.MiddlewareFunc {
	debugID := utils.GetEnvUUIDOptional("SAUTH_DEBUG_CLIENT_ID")
	debugSecret := utils.GetEnvOptional("SAUTH_DEBUG_CLIENT_SECRET")

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			err := authenticateClient(ctx, debugID, debugSecret)
			if errors.Is(err, ErrorCredentialMissing) {
				return ctx.String(http.StatusUnauthorized, "Unauthorized")
			}
			if err != nil {
				return respondAuthError(ctx, err)
			}
			if forwardErr := forwardUserToken(ctx); forwardErr != nil {
				return respondAuthError(ctx, forwardErr)
			}
			return next(ctx)
		}
	}
}

func authenticateClient(ctx echo.Context, debugID *uuid.UUID, debugSecret string) error {
	keyHeader := ctx.Request().Header.Get("SAuth-Client-Key")
	secretHeader := ctx.Request().Header.Get("SAuth-Client-Secret")

	if keyHeader == "" || secretHeader == "" {
		return ErrorCredentialMissing
	}

	if debugID != nil && debugSecret != "" {
		if keyHeader == debugID.String() && secretHeader == debugSecret {
			ctx.Set(UserIDKey, *debugID)
			return nil
		}
		return fmt.Errorf("debug client bypass mismatch: %w", ErrorUnauthorized)
	}

	clientKey, err := uuid.Parse(keyHeader)
	if err != nil {
		return fmt.Errorf("parse client key: %w: %w", err, ErrorUnauthorized)
	}

	samnaClient, err := db.Instance().GetClient(ctx.Request().Context(), clientKey)
	if err != nil {
		return fmt.Errorf("lookup client: %w: %w", err, ErrorUnauthorized)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(samnaClient.SecretHash), []byte(secretHeader)); err != nil {
		return fmt.Errorf("verify client secret: %w: %w", err, ErrorUnauthorized)
	}

	ctx.Set(UserIDKey, samnaClient.ID)
	return nil
}

func forwardUserToken(ctx echo.Context) error {
	token := ctx.Request().Header.Get(userTokenHeader)
	if token == "" {
		return nil
	}
	user, err := client.PostUserTokenValidate(ctx.Request().Context(), token)
	if err != nil {
		return fmt.Errorf("validate forwarded user token: %w: %w", err, ErrorUnauthorized)
	}
	if user.UserID == uuid.Nil {
		return ErrorUnauthorized
	}
	ctx.Set(UserIDKey, user.UserID)
	return nil
}
