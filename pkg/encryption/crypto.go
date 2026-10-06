package encryption

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

type MasterKeyPair struct {
	PrivateKey *rsa.PrivateKey
	Seed       []byte
}

func GenerateMasterKey() (*MasterKeyPair, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, err
	}

	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}

	return &MasterKeyPair{
		PrivateKey: privateKey,
		Seed:       seed,
	}, nil
}

func ExportMasterKeyPair(masterKey *MasterKeyPair) (string, string, error) {
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(masterKey.PrivateKey)
	privateKeyBase64 := base64.RawURLEncoding.EncodeToString(privateKeyBytes)
	seedHex := hex.EncodeToString(masterKey.Seed)
	return privateKeyBase64, seedHex, nil
}

func ImportMasterKeyPair(privateKeyBase64 string, seedHex string) (*MasterKeyPair, error) {
	privateKeyBytes, err := base64.RawURLEncoding.DecodeString(privateKeyBase64)
	if err != nil {
		return nil, err
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(privateKeyBytes)
	if err != nil {
		return nil, err
	}

	seed, err := hex.DecodeString(seedHex)
	if err != nil {
		return nil, err
	}

	return &MasterKeyPair{
		PrivateKey: privateKey,
		Seed:       seed,
	}, nil
}

func GenerateAppSecret(seed []byte, version int) (string, string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	body := make([]byte, 0, 48)
	buf := make([]byte, 64)
	for len(body) < 48 {
		if _, err := rand.Read(buf); err != nil {
			return "", "", err
		}
		for _, b := range buf {
			if b < 248 {
				body = append(body, charset[b%62])
				if len(body) == 48 {
					break
				}
			}
		}
	}
	raw := fmt.Sprintf("sk-saa-app%02d-%s", version, string(body))
	hash := hashAppSecret(seed, raw)
	return raw, hash, nil
}

func ValidateAppSecret(seed []byte, secret string, storedHash string) bool {
	return hmac.Equal([]byte(hashAppSecret(seed, secret)), []byte(storedHash))
}

func hashAppSecret(seed []byte, secret string) string {
	h := hmac.New(sha256.New, seed)
	h.Write([]byte(secret))
	return hex.EncodeToString(h.Sum(nil))
}

func PublicKeyToString(publicKey *rsa.PublicKey) (string, error) {
	derPkix, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(derPkix), nil
}

func (masterKey *MasterKeyPair) GetPublicKey() (string, error) {
	return PublicKeyToString(&masterKey.PrivateKey.PublicKey)
}

func SignPayload(payload []byte, privateKey *rsa.PrivateKey) (string, error) {
	hashed := sha256.Sum256(payload)
	sig, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hashed[:])
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(sig), nil
}

const (
	argonMemory      = 64 * 1024
	argonIterations  = 3
	argonParallelism = 4
	argonKeyLength   = 32
	argonSaltLength  = 16
)

func HashPassword(password string, pepper []byte) (string, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	input := append(pepper, []byte(password)...)
	hash := argon2.IDKey(input, salt, argonIterations, argonMemory, argonParallelism, argonKeyLength)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonIterations, argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func VerifyPassword(password string, encodedHash string, pepper []byte) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return false, fmt.Errorf("invalid hash format")
	}

	var memory uint32
	var iterations uint32
	var parallelism uint8
	_, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism)
	if err != nil {
		return false, err
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}

	storedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}

	input := append(pepper, []byte(password)...)
	computed := argon2.IDKey(input, salt, iterations, memory, parallelism, uint32(len(storedHash)))

	if subtle.ConstantTimeCompare(storedHash, computed) == 1 {
		return true, nil
	}
	return false, nil
}
