package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const atRestPrefix = "enc:v1:"

var ErrMasterKey = errors.New("SAUTH_MASTER_KEY must be 32 random bytes, base64 encoded")
var ErrNotWrapped = errors.New("value is not wrapped at rest")

func GetMasterKey() ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(os.Getenv("SAUTH_MASTER_KEY"))
	if err != nil || len(key) != 32 {
		return nil, ErrMasterKey
	}
	return key, nil
}

func IsWrapped(stored string) bool {
	return strings.HasPrefix(stored, atRestPrefix)
}

func WrapAtRest(plaintext string) (string, error) {
	key, err := GetMasterKey()
	if err != nil {
		return "", err
	}
	gcm, err := getMasterGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("at rest nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return atRestPrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func UnwrapAtRest(stored string) (string, error) {
	if !IsWrapped(stored) {
		return "", ErrNotWrapped
	}
	key, err := GetMasterKey()
	if err != nil {
		return "", err
	}
	body, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(stored, atRestPrefix))
	if err != nil {
		return "", fmt.Errorf("at rest decode: %w", err)
	}
	gcm, err := getMasterGCM(key)
	if err != nil {
		return "", err
	}
	if len(body) < gcm.NonceSize() {
		return "", errors.New("at rest ciphertext truncated")
	}
	plain, err := gcm.Open(nil, body[:gcm.NonceSize()], body[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("at rest open: %w", err)
	}
	return string(plain), nil
}

func getMasterGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("at rest cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("at rest gcm: %w", err)
	}
	return gcm, nil
}
