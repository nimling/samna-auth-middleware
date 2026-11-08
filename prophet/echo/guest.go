package echo

import (
	"crypto/rsa"
	"errors"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/pkg/encryption"
	"github.com/nimling/samna-auth-middleware/v2/pkg/respond"
	sdb "github.com/nimling/samna-auth-middleware/v2/prophet/sdb"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

type BodyGuest struct {
	ClaimID uuid.UUID `json:"claim_id"`
	UserID  uuid.UUID `json:"user_id"`
}

type GuestTokenResponse struct {
	AccessToken     string     `json:"access_token"`
	AccessExpiresIn int        `json:"access_expires_in"`
	TokenType       string     `json:"token_type"`
	UserID          uuid.UUID  `json:"user_id"`
	ClaimID         uuid.UUID  `json:"claim_id"`
	StartsAt        *time.Time `json:"starts_at,omitempty"`
	EndsAt          *time.Time `json:"ends_at,omitempty"`
}

func PostGuestToken(ctx echo.Context) error {
	var body BodyGuest
	if err := ctx.Bind(&body); err != nil {
		return respond.Error(ctx, http.StatusBadRequest, "Bad request")
	}
	if body.ClaimID == uuid.Nil {
		return respond.Error(ctx, http.StatusBadRequest, "claim_id is required")
	}
	if body.UserID == uuid.Nil {
		return respond.Error(ctx, http.StatusBadRequest, "user_id is required")
	}

	appID, _ := ctx.Get("app_id").(uuid.UUID)
	appSlug, _ := ctx.Get("app_slug").(string)
	masterKey, _ := ctx.Get("app_key").(encryption.MasterKeyPair)
	grant, err := sdb.PutGuestUser(ctx.Request().Context(), appID, body.ClaimID, body.UserID)
	if errors.Is(err, shared.ErrNotFound) {
		return respond.Error(ctx, http.StatusNotFound, "Not found")
	}
	if errors.Is(err, shared.ErrNoAccess) {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
	if err != nil {
		ctx.Logger().Errorf("PostGuestToken: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	guestUserID := sdb.GetGuestUserID(appID, body.ClaimID)
	accessToken, expiresIn, err := signGuestAccessToken(masterKey.PrivateKey, appID, appSlug, guestUserID, utils.Cfg().Endpoint, grant.StartsAt, grant.EndsAt)
	if err != nil {
		ctx.Logger().Errorf("PostGuestToken: sign access token: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	return respond.JSON(ctx, http.StatusOK, &GuestTokenResponse{
		AccessToken:     accessToken,
		AccessExpiresIn: expiresIn,
		TokenType:       "Bearer",
		UserID:          guestUserID,
		ClaimID:         body.ClaimID,
		StartsAt:        grant.StartsAt,
		EndsAt:          grant.EndsAt,
	}, 1)
}

type guestClaims struct {
	jwt.RegisteredClaims
}

func signGuestAccessToken(privateKey *rsa.PrivateKey, appID uuid.UUID, appSlug string, userID uuid.UUID, issuer string, startsAt, endsAt *time.Time) (string, int, error) {
	now := time.Now()
	nbf := now
	if startsAt != nil && startsAt.After(now) {
		nbf = *startsAt
	}

	claims := guestClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			Subject:   userID.String(),
			Issuer:    issuer,
			Audience:  jwt.ClaimStrings{appSlug},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(nbf),
		},
	}

	lifetime := 900
	if v, err := strconv.Atoi(os.Getenv("SAUTH_ACCESS_TOKEN_DURATION")); err == nil && v > 0 {
		lifetime = v
	}
	expiresAt := nbf.Add(time.Duration(lifetime) * time.Second)
	if endsAt != nil && endsAt.Before(expiresAt) {
		expiresAt = *endsAt
	}
	claims.ExpiresAt = jwt.NewNumericDate(expiresAt)
	expiresIn := 0
	if diff := time.Until(expiresAt); diff > 0 {
		expiresIn = int(diff.Seconds())
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = appID.String()

	signed, err := token.SignedString(privateKey)
	if err != nil {
		return "", 0, err
	}
	return signed, expiresIn, nil
}
