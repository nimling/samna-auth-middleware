package smw_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/nimling/samna-auth-middleware/dpop"
	"github.com/nimling/samna-auth-middleware/smw"
)

const (
	testIssuer = "https://auth.example.com"
	testSlug   = "nimployer"
)

func mustKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func signAccess(t *testing.T, key *rsa.PrivateKey, issuer string, audience []string) string {
	t.Helper()
	claims := smw.TokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "11111111-1111-1111-1111-111111111111",
			Issuer:    issuer,
			Audience:  jwt.ClaimStrings(audience),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

func keyFunc(key *rsa.PrivateKey) jwt.Keyfunc {
	return func(t *jwt.Token) (interface{}, error) {
		return &key.PublicKey, nil
	}
}

func TestValidateTokenAudienceAccept(t *testing.T) {
	key := mustKey(t)
	token := signAccess(t, key, testIssuer, []string{testSlug})
	config := &smw.AuthMiddlewareConfig{ClientSlug: testSlug, ExpectedTokenIssuer: testIssuer}

	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/resource", nil)
	user, err := smw.ValidateToken(req, token, config, keyFunc(key))
	if err != nil {
		t.Fatalf("expected valid token, got %v", err)
	}
	if user.ID.String() != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("unexpected subject %v", user.ID)
	}
}

func TestValidateTokenAudienceReject(t *testing.T) {
	key := mustKey(t)
	token := signAccess(t, key, testIssuer, []string{"other-app"})
	config := &smw.AuthMiddlewareConfig{ClientSlug: testSlug, ExpectedTokenIssuer: testIssuer}

	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/resource", nil)
	if _, err := smw.ValidateToken(req, token, config, keyFunc(key)); err == nil {
		t.Fatalf("expected audience rejection")
	}
}

func TestValidateTokenIssuerReject(t *testing.T) {
	key := mustKey(t)
	token := signAccess(t, key, "https://evil.example.com", []string{testSlug})
	config := &smw.AuthMiddlewareConfig{ClientSlug: testSlug, ExpectedTokenIssuer: testIssuer}

	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/resource", nil)
	if _, err := smw.ValidateToken(req, token, config, keyFunc(key)); err == nil {
		t.Fatalf("expected issuer rejection")
	}
}

type fakeStore struct {
	seen map[string]bool
}

func (s *fakeStore) CheckAndStore(_ context.Context, jti string, _ time.Duration) (bool, error) {
	if s.seen[jti] {
		return false, nil
	}
	s.seen[jti] = true
	return true, nil
}

func signProof(t *testing.T, key *rsa.PrivateKey, method, url, jti string) string {
	t.Helper()
	pub, err := jwk.FromRaw(&key.PublicKey)
	if err != nil {
		t.Fatalf("jwk from raw: %v", err)
	}
	buf, err := json.Marshal(pub)
	if err != nil {
		t.Fatalf("marshal jwk: %v", err)
	}
	var jwkMap map[string]interface{}
	if err := json.Unmarshal(buf, &jwkMap); err != nil {
		t.Fatalf("unmarshal jwk: %v", err)
	}

	claims := jwt.MapClaims{
		"htm": method,
		"htu": url,
		"iat": time.Now().Unix(),
		"jti": jti,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["typ"] = dpop.Typ
	token.Header["jwk"] = jwkMap
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign proof: %v", err)
	}
	return signed
}

func TestDPoPVerifyEmptyHeader(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/resource", nil)
	proof, err := dpop.Verify(req, "", "", &fakeStore{seen: map[string]bool{}})
	if err != nil {
		t.Fatalf("empty header should not error, got %v", err)
	}
	if proof != nil {
		t.Fatalf("empty header should yield nil proof")
	}
}

func TestDPoPVerifyReplayRejected(t *testing.T) {
	key := mustKey(t)
	store := &fakeStore{seen: map[string]bool{}}
	url := "https://api.example.com/resource"
	proof := signProof(t, key, http.MethodPost, url, "jti-1")

	req, _ := http.NewRequest(http.MethodPost, url, nil)
	if _, err := dpop.Verify(req, proof, "", store); err != nil {
		t.Fatalf("first proof should verify, got %v", err)
	}

	req2, _ := http.NewRequest(http.MethodPost, url, nil)
	if _, err := dpop.Verify(req2, proof, "", store); err == nil {
		t.Fatalf("replayed jti should be rejected")
	}
}
