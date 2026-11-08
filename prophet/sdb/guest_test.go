package sdb

import (
	"crypto/rand"
	"crypto/rsa"
	"slices"
	"testing"

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
