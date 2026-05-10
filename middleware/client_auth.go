package middleware

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"

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
	clientID, clientSecret := readClientCredentials(ctx)
	if clientID == "" || clientSecret == "" {
		return ErrorCredentialMissing
	}

	if debugID != nil && debugSecret != "" {
		if clientID == debugID.String() && clientSecret == debugSecret {
			ctx.Set(UserIDKey, *debugID)
			return nil
		}
		return fmt.Errorf("debug client bypass mismatch: %w", ErrorUnauthorized)
	}

	clientUUID, err := uuid.Parse(clientID)
	if err != nil {
		return fmt.Errorf("parse client_id: %w: %w", err, ErrorUnauthorized)
	}

	samnaClient, err := db.Instance().GetClient(ctx.Request().Context(), clientUUID)
	if err != nil {
		return fmt.Errorf("lookup client: %w: %w", err, ErrorUnauthorized)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(samnaClient.SecretHash), []byte(clientSecret)); err != nil {
		return fmt.Errorf("verify client_secret: %w: %w", err, ErrorUnauthorized)
	}

	ctx.Set(UserIDKey, samnaClient.ClientID)
	return nil
}

func readClientCredentials(ctx echo.Context) (string, string) {
	auth := ctx.Request().Header.Get(echo.HeaderAuthorization)
	if strings.HasPrefix(auth, "Basic ") {
		payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "Basic "))
		if err == nil {
			parts := strings.SplitN(string(payload), ":", 2)
			if len(parts) == 2 {
				return parts[0], parts[1]
			}
		}
	}
	return ctx.FormValue("client_id"), ctx.FormValue("client_secret")
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
