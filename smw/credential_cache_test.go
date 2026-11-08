package smw

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"testing"

	"github.com/google/uuid"
	pecho "github.com/nimling/samna-auth-middleware/v2/prophet/echo"
	"github.com/nimling/samna-auth-middleware/v2/shared"
	"golang.org/x/crypto/bcrypt"
)

func hashSecret(t *testing.T, secret string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash secret: %v", err)
	}
	return string(hash)
}

func TestVerifyClientSecretKeepsADigestUntilTheHashRotates(t *testing.T) {
	client := &shared.SamnaClient{ClientID: uuid.New(), SecretHash: hashSecret(t, "first")}

	if err := verifyClientSecret(client, "first"); err != nil {
		t.Fatalf("the stored secret must verify: %v", err)
	}
	cached, ok := verifiedSecrets.Load(client.ClientID)
	if !ok || cached.(verifiedSecret).secretHash != client.SecretHash {
		t.Fatal("a verified secret must be kept as a digest against the hash it passed")
	}
	if err := verifyClientSecret(client, "first"); err != nil {
		t.Fatalf("the kept digest must verify the same secret again: %v", err)
	}
	if err := verifyClientSecret(client, "guess"); err == nil {
		t.Fatal("a secret other than the kept one must be refused")
	}

	client.SecretHash = hashSecret(t, "second")
	if err := verifyClientSecret(client, "first"); err == nil {
		t.Fatal("once the secret_hash rotates the old secret must stop working")
	}
	if err := verifyClientSecret(client, "second"); err != nil {
		t.Fatalf("the rotated secret must verify: %v", err)
	}
	if err := verifyClientSecret(client, "first"); err == nil {
		t.Fatal("the old secret must stay refused after the rotated one is kept")
	}
}

func getTestApp(t *testing.T) pecho.SamnaApp {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pecho.SamnaApp{
		ID:          uuid.New(),
		PrivateKey:  base64.RawURLEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(key)),
		PrivateSeed: "00112233445566778899aabbccddeeff",
	}
}

func TestGetAppKeyImportsTheKeyPairOnce(t *testing.T) {
	app := getTestApp(t)

	first, err := getAppKey(app)
	if err != nil {
		t.Fatalf("import key pair: %v", err)
	}
	again, err := getAppKey(app)
	if err != nil || again != first {
		t.Fatalf("a second request must reuse the imported key pair, got %p and %p (%v)", first, again, err)
	}

	app.PrivateSeed = "ffeeddccbbaa99887766554433221100"
	rotated, err := getAppKey(app)
	if err != nil {
		t.Fatalf("import rotated key pair: %v", err)
	}
	if rotated == first || rotated.Seed[0] != 0xff {
		t.Fatal("a changed key pair must be imported again")
	}

	app.PrivateKey = "not a key"
	if _, err := getAppKey(app); err == nil {
		t.Fatal("a key pair that no longer imports must fail rather than answer from the cache")
	}
}
