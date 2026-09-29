package smw

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	sdb "github.com/nimling/samna-auth-middleware/disciple/sdb"
	"github.com/nimling/samna-auth-middleware/shared"
	"github.com/nimling/samna-auth-middleware/utils"
	"golang.org/x/crypto/bcrypt"
)

const (
	userTokenHeader    = "SAuth-User-Token"
	userExternalHeader = "SAuth-User-ID"
)

// ClientAuthMiddleware authenticates service to service callers by their client
// credentials. Mount it on routes that machines rather than browsers reach.
func ClientAuthMiddleware() echo.MiddlewareFunc {
	debugUserID := parseUUID(utils.Cfg().DebugUserID)
	debugClients := parseDebugClients()

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			if debugUserID != nil {
				ctx.Set(UserIDKey, *debugUserID)
				if forwardErr := forwardUserToken(ctx); forwardErr != nil {
					return respondAuthError(ctx, forwardErr)
				}
				if forwardErr := forwardUserExternal(ctx); forwardErr != nil {
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
			if forwardErr := forwardUserExternal(ctx); forwardErr != nil {
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

	samnaClient, err := sdb.Instance().GetClient(ctx.Request().Context(), clientUUID)
	if err != nil {
		return fmt.Errorf("lookup client: %w: %w", err, ErrorUnauthorized)
	}

	if err := verifyClientSecret(samnaClient, clientSecret); err != nil {
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
	user, ownerID, err := sdb.Instance().ValidateToken(ctx.Request().Context(), token)
	if err != nil {
		return fmt.Errorf("validate forwarded user token: %w: %w", err, ErrorUserTokenNotRecognized)
	}
	if user == nil || user.UserID == uuid.Nil {
		return ErrorUserTokenNotRecognized
	}
	clientID, _ := ctx.Get(UserIDKey).(uuid.UUID)
	if ownerID == uuid.Nil || !sdb.Instance().HasAccess(ctx.Request().Context(), clientID, ownerID, shared.OBJECT_TYPE_ORGANIZATION, shared.ACCESS_READ) {
		return ErrorUserTokenNotRecognized
	}
	ctx.Set(UserIDKey, user.UserID)
	return nil
}

func forwardUserExternal(ctx echo.Context) error {
	externalID := ctx.Request().Header.Get(userExternalHeader)
	if externalID == "" {
		return nil
	}
	if ctx.Request().Header.Get(userTokenHeader) != "" {
		return ErrorForbidden
	}
	clientID, _ := ctx.Get(UserIDKey).(uuid.UUID)
	user, err := sdb.Instance().ResolveExternalUser(ctx.Request().Context(), clientID, externalID)
	if err != nil {
		return fmt.Errorf("resolve external user id: %w: %w", err, ErrorUserExternalNotRecognized)
	}
	if user == nil || user.UserID == uuid.Nil {
		return ErrorUserExternalNotRecognized
	}
	ctx.Set(UserIDKey, user.UserID)
	return nil
}

var verifiedSecrets sync.Map

type verifiedSecret struct {
	digest     [sha256.Size]byte
	secretHash string
}

func verifyClientSecret(client *shared.SamnaClient, secret string) error {
	digest := sha256.Sum256([]byte(secret))
	if v, ok := verifiedSecrets.Load(client.ClientID); ok {
		known := v.(verifiedSecret)
		if known.secretHash == client.SecretHash && subtle.ConstantTimeCompare(known.digest[:], digest[:]) == 1 {
			return nil
		}
	}
	if err := bcrypt.CompareHashAndPassword([]byte(client.SecretHash), []byte(secret)); err != nil {
		return err
	}
	verifiedSecrets.Store(client.ClientID, verifiedSecret{digest: digest, secretHash: client.SecretHash})
	return nil
}
