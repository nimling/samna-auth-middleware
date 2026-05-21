package middleware

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/db"
	"golang.org/x/crypto/bcrypt"
)

const userTokenHeader = "SAuth-User-Token"

func ClientAuthMiddleware() echo.MiddlewareFunc {
	debugUserID := getEnvUUID("SAUTH_DEBUG_USER_ID")
	debugClients := parseDebugClients()

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			if debugUserID != nil {
				ctx.Set(UserIDKey, *debugUserID)
				if forwardErr := forwardUserToken(ctx); forwardErr != nil {
					return respondAuthError(ctx, forwardErr)
				}
				return next(ctx)
			}

			err := authenticateClient(ctx, debugClients)
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

func authenticateClient(ctx echo.Context, debugClients map[string]DebugClient) error {
	clientID, clientSecret := readClientCredentials(ctx)
	if clientID == "" || clientSecret == "" {
		return ErrorCredentialMissing
	}

	if len(debugClients) > 0 {
		for _, dc := range debugClients {
			if dc.Status != nil && *dc.Status != "" && *dc.Status != "active" {
				continue
			}
			if dc.ExpiresAt != nil && time.Now().After(*dc.ExpiresAt) {
				continue
			}
			if clientID == dc.ClientID.String() && clientSecret == dc.ClientSecret {
				ctx.Set(UserIDKey, dc.ClientID)
				return nil
			}
		}
		return fmt.Errorf("debug client basic auth mismatch: %w", ErrorUnauthorized)
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
	user, ownerID, err := db.Instance().ValidateToken(ctx.Request().Context(), token)
	if err != nil {
		return fmt.Errorf("validate forwarded user token: %w: %w", err, ErrorUserTokenNotRecognized)
	}
	if user == nil || user.UserID == uuid.Nil {
		return ErrorUserTokenNotRecognized
	}
	if ownerID != uuid.Nil {
		clientID, _ := ctx.Get(UserIDKey).(uuid.UUID)
		if !db.Instance().HasAccess(ctx.Request().Context(), clientID, ownerID, db.OBJECT_TYPE_ORGANIZATION, db.ACCESS_READ) {
			return ErrorUserTokenNotRecognized
		}
	}
	ctx.Set(UserIDKey, user.UserID)
	return nil
}
