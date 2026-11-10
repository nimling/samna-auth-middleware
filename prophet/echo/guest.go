package echo

import (
	"crypto/rsa"
	"encoding/json"
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
	"github.com/nimling/samna-auth-middleware/v2/sauth"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

func PostGuestToken(ctx echo.Context) error {
	var body shared.BodyGuest
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
	raw, err := sdb.PutGuestUser(ctx.Request().Context(), appID, body)
	if errors.Is(err, shared.ErrNotFound) {
		return respond.Error(ctx, http.StatusNotFound, "Not found")
	}
	if errors.Is(err, shared.ErrNoAccess) {
		return respond.Error(ctx, http.StatusForbidden, "Forbidden")
	}
	if errors.Is(err, shared.ErrBadCode) {
		return respond.Error(ctx, http.StatusBadRequest, err.Error())
	}
	if err != nil {
		ctx.Logger().Errorf("PostGuestToken: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	var resp shared.GuestTokenResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		ctx.Logger().Errorf("PostGuestToken: read guest: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	var user shared.SamnaUser
	var grant shared.UserClaim
	if err := sauth.Unmarshal(resp.SamnaUser, &user); err != nil {
		ctx.Logger().Errorf("PostGuestToken: read guest user: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}
	if err := sauth.Unmarshal(resp.UserClaim, &grant); err != nil {
		ctx.Logger().Errorf("PostGuestToken: read guest grant: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	accessToken, expiresIn, err := signGuestAccessToken(masterKey.PrivateKey, appID, appSlug, user.UserID, utils.Cfg().Endpoint, grant.StartsAt, grant.EndsAt)
	if err != nil {
		ctx.Logger().Errorf("PostGuestToken: sign access token: %v", err)
		return respond.Error(ctx, http.StatusInternalServerError, "Internal server error")
	}

	resp.AccessToken, resp.AccessExpiresIn, resp.TokenType = accessToken, expiresIn, "Bearer"
	resp.UserID, resp.ClaimID, resp.StartsAt, resp.EndsAt = user.UserID, body.ClaimID, grant.StartsAt, grant.EndsAt
	return respond.JSON(ctx, http.StatusOK, &resp, 1)
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
