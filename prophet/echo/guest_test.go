package echo

import (
	"crypto/rand"
	"crypto/rsa"
	"slices"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestSignGuestAccessTokenCarriesTheAppSlugAsAudience(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signed, _, err := signGuestAccessToken(key, uuid.New(), "hvergel", uuid.New(), "https://auth.example", nil, nil)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	var claims jwt.RegisteredClaims
	if _, err := jwt.ParseWithClaims(signed, &claims, func(*jwt.Token) (any, error) { return &key.PublicKey, nil }); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !slices.Contains(claims.Audience, "hvergel") {
		t.Errorf("aud = %v, want it to hold hvergel, because smw.ValidateToken refuses a token whose aud lacks the disciple's slug", claims.Audience)
	}
}

func TestEveryGuestTokenExpires(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	t.Setenv("SAUTH_ACCESS_TOKEN_DURATION", "900")
	read := func(endsAt *time.Time) (jwt.RegisteredClaims, int) {
		signed, expiresIn, err := signGuestAccessToken(key, uuid.New(), "hvergel", uuid.New(), "https://auth.example", nil, endsAt)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		var claims jwt.RegisteredClaims
		if _, err := jwt.ParseWithClaims(signed, &claims, func(*jwt.Token) (any, error) { return &key.PublicKey, nil }); err != nil {
			t.Fatalf("parse: %v", err)
		}
		return claims, expiresIn
	}

	claims, expiresIn := read(nil)
	if claims.ExpiresAt == nil || claims.ExpiresAt.After(time.Now().Add(901*time.Second)) || expiresIn <= 0 || expiresIn > 900 {
		t.Fatalf("a guest token without a grant end lives the access token duration, got %v and %d s", claims.ExpiresAt, expiresIn)
	}

	soon := time.Unix(time.Now().Add(time.Minute).Unix(), 0)
	claims, expiresIn = read(&soon)
	if claims.ExpiresAt == nil || !claims.ExpiresAt.Equal(soon) || expiresIn > 60 {
		t.Fatalf("a grant ending sooner caps the token, got %v and %d s", claims.ExpiresAt, expiresIn)
	}
}
