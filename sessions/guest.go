package sessions

import (
	"context"
	"crypto/rsa"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/nimling/samna-auth-middleware/client"
	"github.com/nimling/samna-auth-middleware/db"
	"github.com/nimling/samna-auth-middleware/pkg/encryption"
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

func IssueGuestToken(ctx context.Context, claimID uuid.UUID, issuer string) (*GuestTokenResponse, error) {
	app, err := db.Instance().GetApp(ctx)
	if err != nil {
		return nil, fmt.Errorf("get app: %w", err)
	}

	var claim db.Claim
	err = db.Instance().NewSelect().
		Model(&claim).
		Where("id = ?", claimID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("read claim: %w", err)
	}

	guestUserID := uuid.NewSHA1(app.ID, []byte("guest:"+claimID.String()))

	var existingClaim db.UserClaim
	err = db.Instance().NewSelect().
		Model(&existingClaim).
		Where("claim_id = ?", claimID).
		Where("user_id = ?", guestUserID).
		Where("app_id = ?", app.ID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx)

	if errors.Is(err, sql.ErrNoRows) {
		guestType := "guest"
		guestFirst := "Guest"
		_, postErr := client.PostUser(ctx, db.SamnaUser{
			UserID:    guestUserID,
			AppID:     app.ID,
			FirstName: &guestFirst,
			Type:      &guestType,
		})
		if postErr != nil {
			return nil, fmt.Errorf("upsert guest user: %w", postErr)
		}

		newClaim, postClaimErr := client.PostUserClaim(ctx, claimID, db.UserClaim{
			AppID:   app.ID,
			ClaimID: claimID,
			UserID:  guestUserID,
			OwnerID: claim.OwnerID,
		})
		if postClaimErr != nil {
			return nil, fmt.Errorf("assign guest claim: %w", postClaimErr)
		}
		existingClaim = newClaim
	} else if err != nil {
		return nil, fmt.Errorf("read user_claim: %w", err)
	}

	masterKey, err := encryption.ImportMasterKeyPair(app.PrivateKey, app.PrivateSeed)
	if err != nil {
		return nil, fmt.Errorf("import master key: %w", err)
	}

	if issuer == "" {
		issuer = os.Getenv("SAUTH_ENDPOINT")
	}

	accessToken, expiresIn, err := signGuestAccessToken(masterKey.PrivateKey, app.ID, guestUserID, issuer, existingClaim.StartsAt, existingClaim.EndsAt)
	if err != nil {
		return nil, fmt.Errorf("sign access token: %w", err)
	}

	return &GuestTokenResponse{
		AccessToken:     accessToken,
		AccessExpiresIn: expiresIn,
		TokenType:       "Bearer",
		UserID:          guestUserID,
		ClaimID:         claimID,
		StartsAt:        existingClaim.StartsAt,
		EndsAt:          existingClaim.EndsAt,
	}, nil
}

type guestClaims struct {
	jwt.RegisteredClaims
}

func signGuestAccessToken(privateKey *rsa.PrivateKey, appID, userID uuid.UUID, issuer string, startsAt, endsAt *time.Time) (string, int, error) {
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
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(nbf),
		},
	}

	expiresIn := 0
	if endsAt != nil {
		claims.ExpiresAt = jwt.NewNumericDate(*endsAt)
		diff := time.Until(*endsAt)
		if diff > 0 {
			expiresIn = int(diff.Seconds())
		}
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = appID.String()

	signed, err := token.SignedString(privateKey)
	if err != nil {
		return "", 0, err
	}
	return signed, expiresIn, nil
}
