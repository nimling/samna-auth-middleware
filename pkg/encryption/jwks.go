package encryption

import (
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

func GenerateJWKS(publicKey *rsa.PublicKey, kid string) ([]byte, error) {
	if publicKey == nil {
		return nil, errors.New("public key is nil")
	}
	if kid == "" {
		return nil, errors.New("kid is empty")
	}

	key, err := jwk.FromRaw(publicKey)
	if err != nil {
		return nil, fmt.Errorf("create JWK from raw key: %w", err)
	}

	if err = key.Set(jwk.KeyIDKey, kid); err != nil {
		return nil, fmt.Errorf("set kid: %w", err)
	}
	if err = key.Set(jwk.AlgorithmKey, jwa.RS256); err != nil {
		return nil, fmt.Errorf("set alg: %w", err)
	}
	if err = key.Set(jwk.KeyUsageKey, jwk.ForSignature); err != nil {
		return nil, fmt.Errorf("set use: %w", err)
	}

	set := jwk.NewSet()
	if err = set.AddKey(key); err != nil {
		return nil, fmt.Errorf("add key: %w", err)
	}

	b, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal JWKS: %w", err)
	}

	return b, nil
}
