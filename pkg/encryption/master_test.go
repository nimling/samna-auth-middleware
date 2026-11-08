package encryption

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func getTestMasterKey(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(key)
}

func TestWrapAtRestRoundTrips(t *testing.T) {
	t.Setenv("SAUTH_MASTER_KEY", getTestMasterKey(t))
	wrapped, err := WrapAtRest("a6678960762ea4b1")
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if !IsWrapped(wrapped) || strings.Contains(wrapped, "a6678960762ea4b1") {
		t.Fatalf("the stored form must be the prefixed ciphertext, got %q", wrapped)
	}
	again, err := WrapAtRest("a6678960762ea4b1")
	if err != nil || again == wrapped {
		t.Fatal("every wrap must take a fresh nonce")
	}
	plain, err := UnwrapAtRest(wrapped)
	if err != nil || plain != "a6678960762ea4b1" {
		t.Fatalf("unwrap: %q, %v", plain, err)
	}
	t.Setenv("SAUTH_MASTER_KEY", getTestMasterKey(t))
	if _, err := UnwrapAtRest(wrapped); err == nil {
		t.Fatal("another master key must not open the value")
	}
}

func TestMasterKeyIsExactlyThirtyTwoBase64Bytes(t *testing.T) {
	short := make([]byte, 16)
	for _, key := range []string{
		"",
		base64.StdEncoding.EncodeToString(short),
		hex.EncodeToString(make([]byte, 32)),
		"0123456789abcdef0123456789abcdef",
		"not base64 at all !",
	} {
		t.Setenv("SAUTH_MASTER_KEY", key)
		if _, err := GetMasterKey(); !errors.Is(err, ErrMasterKey) {
			t.Fatalf("key %q must be refused, got %v", key, err)
		}
		if _, err := WrapAtRest("plain"); !errors.Is(err, ErrMasterKey) {
			t.Fatalf("wrap under key %q must refuse rather than store plaintext, got %v", key, err)
		}
	}
	t.Setenv("SAUTH_MASTER_KEY", getTestMasterKey(t))
	if _, err := GetMasterKey(); err != nil {
		t.Fatalf("32 random bytes in base64: %v", err)
	}
}

func TestUnwrapAtRestRefusesPlaintext(t *testing.T) {
	t.Setenv("SAUTH_MASTER_KEY", getTestMasterKey(t))
	if _, err := UnwrapAtRest("a6678960762ea4b1"); !errors.Is(err, ErrNotWrapped) {
		t.Fatalf("a plaintext value must be refused, got %v", err)
	}
}

func TestImportMasterKeyPairOpensWrappedKeys(t *testing.T) {
	t.Setenv("SAUTH_MASTER_KEY", getTestMasterKey(t))
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	privateKey := base64.RawURLEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(key))
	seed := "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	if _, err := ImportMasterKeyPair(privateKey, seed); !errors.Is(err, ErrNotWrapped) {
		t.Fatalf("plaintext key material must be refused, got %v", err)
	}
	wrappedKey, err := WrapAtRest(privateKey)
	if err != nil {
		t.Fatalf("wrap key: %v", err)
	}
	wrappedSeed, err := WrapAtRest(seed)
	if err != nil {
		t.Fatalf("wrap seed: %v", err)
	}
	pair, err := ImportMasterKeyPair(wrappedKey, wrappedSeed)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if hex.EncodeToString(pair.Seed) != seed || !pair.PrivateKey.Equal(key) {
		t.Fatal("the imported pair must be the wrapped one")
	}
}
