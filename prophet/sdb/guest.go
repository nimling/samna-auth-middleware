package sdb

import (
	"context"
	"crypto/rsa"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/nimling/samna-auth-middleware/pkg/encryption"
	"github.com/nimling/samna-auth-middleware/shared"
	"github.com/nimling/samna-auth-middleware/utils"
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
	d := instance

	app, err := d.GetApp(ctx)
	if err != nil {
		return nil, fmt.Errorf("get app: %w", err)
	}

	var claim shared.Claim
	err = d.NewSelect().
		Model(&claim).
		ExcludeColumn("sa_deleted_at").
		Where("id = ?", claimID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("read claim: %w", err)
	}

	guestUserID := uuid.NewSHA1(app.ID, []byte("guest:"+claimID.String()))

	var existingClaim shared.UserClaim
	err = d.NewSelect().
		Model(&existingClaim).
		ExcludeColumn("sa_deleted_at").
		Where("claim_id = ?", claimID).
		Where("user_id = ?", guestUserID).
		Where("sa_deleted_at IS NULL").
		Limit(1).
		Scan(ctx)

	if errors.Is(err, sql.ErrNoRows) {
		guestType := "guest"
		guestFirst := "Guest"
		guestUser := shared.SamnaUser{
			UserID:    guestUserID,
			FirstName: &guestFirst,
			Type:      &guestType,
		}
		guestClaim := shared.UserClaim{
			ClaimID: claimID,
			UserID:  guestUserID,
			OwnerID: claim.OwnerID,
		}

		userID, _ := ctx.Value("user_id").(uuid.UUID)
		if err := d.NewInsert().Model(&guestUser).Scan(ctx, &guestUser); err != nil {
			return nil, fmt.Errorf("upsert guest user: %w", err)
		}
		ac, accErr := d.MutationAccess(ctx, userID, claimID, shared.OBJECT_TYPE_CLAIM, shared.ACCESS_OWNER)
		if accErr != nil {
			return nil, fmt.Errorf("guest claim access: %w", accErr)
		}
		guestClaim.ClaimID = claimID
		if err := d.NewInsert().Model(&guestClaim).Access(ac).Scan(ctx, &guestClaim); err != nil {
			return nil, fmt.Errorf("assign guest claim: %w", err)
		}
		existingClaim = guestClaim
	} else if err != nil {
		return nil, fmt.Errorf("read user_claim: %w", err)
	}

	masterKey, err := encryption.ImportMasterKeyPair(app.PrivateKey, app.PrivateSeed)
	if err != nil {
		return nil, fmt.Errorf("import master key: %w", err)
	}

	if issuer == "" {
		issuer = utils.Cfg().Endpoint
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
