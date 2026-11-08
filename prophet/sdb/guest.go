package sdb

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/uptrace/bun/driver/pgdriver"

	"github.com/nimling/samna-auth-middleware/v2/pkg/encryption"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

type GuestTokenResponse struct {
	AccessToken     string     `json:"access_token"`
	AccessExpiresIn int        `json:"access_expires_in"`
	TokenType       string     `json:"token_type"`
	UserID          uuid.UUID  `json:"user_id"`
	ClaimID         uuid.UUID  `json:"claim_id"`
	StartsAt        *time.Time `json:"starts_at,omitempty"`
	EndsAt          *time.Time `json:"ends_at,omitempty"`
}

func IssueGuestToken(ctx context.Context, appID uuid.UUID, appSlug string, masterKey encryption.MasterKeyPair, claimID uuid.UUID, userID uuid.UUID, issuer string) (*GuestTokenResponse, error) {
	guestUserID := GetGuestUserID(appID, claimID)

	var payload []byte
	if err := instance.NewRaw("SELECT claimius.upsert_guest_user(?, ?, ?, ?)", appID, claimID, userID, guestUserID).Scan(ctx, &payload); err != nil {
		var pgErr pgdriver.Error
		if errors.As(err, &pgErr) {
			switch pgErr.Field('C') {
			case "P0404":
				return nil, shared.ErrNotFound
			case "P0403":
				return nil, shared.ErrNoAccess
			}
		}
		return nil, fmt.Errorf("upsert guest user: %w", err)
	}
	var guest struct {
		UserClaim struct {
			StartsAt *time.Time `json:"starts_at"`
			EndsAt   *time.Time `json:"ends_at"`
		} `json:"user_claim"`
	}
	if err := json.Unmarshal(payload, &guest); err != nil {
		return nil, fmt.Errorf("read guest grant: %w", err)
	}

	if issuer == "" {
		issuer = utils.Cfg().Endpoint
	}

	accessToken, expiresIn, err := signGuestAccessToken(masterKey.PrivateKey, appID, appSlug, guestUserID, issuer, guest.UserClaim.StartsAt, guest.UserClaim.EndsAt)
	if err != nil {
		return nil, fmt.Errorf("sign access token: %w", err)
	}

	return &GuestTokenResponse{
		AccessToken:     accessToken,
		AccessExpiresIn: expiresIn,
		TokenType:       "Bearer",
		UserID:          guestUserID,
		ClaimID:         claimID,
		StartsAt:        guest.UserClaim.StartsAt,
		EndsAt:          guest.UserClaim.EndsAt,
	}, nil
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
