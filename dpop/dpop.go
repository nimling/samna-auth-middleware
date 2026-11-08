package dpop

import (
	"context"
	"crypto"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lestrrat-go/jwx/v2/jwk"
)

const (
	Header    = "DPoP"
	Typ       = "dpop+jwt"
	ReplayTTL = 5 * time.Minute
	ClockSkew = 60 * time.Second
)

type ReplayStore interface {
	CheckAndStore(ctx context.Context, jti string, ttl time.Duration) (fresh bool, err error)
}

type Proof struct {
	JKT string
	JTI string
}

func Verify(r *http.Request, header, expectedAccessToken string, store ReplayStore) (*Proof, error) {
	if header == "" {
		return nil, nil
	}

	parser := jwt.NewParser(jwt.WithoutClaimsValidation(), jwt.WithValidMethods([]string{"RS256", "PS256", "ES256"}))
	parsed, _, err := parser.ParseUnverified(header, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("parse dpop proof: %w", err)
	}

	typ, _ := parsed.Header["typ"].(string)
	if typ != Typ {
		return nil, errors.New("dpop proof typ must be dpop+jwt")
	}

	jwkRaw, ok := parsed.Header["jwk"].(map[string]interface{})
	if !ok {
		return nil, errors.New("dpop proof missing jwk header")
	}
	jwkBytes, marshalErr := json.Marshal(jwkRaw)
	if marshalErr != nil {
		return nil, fmt.Errorf("marshal dpop jwk: %w", marshalErr)
	}
	publicKey, parseErr := jwk.ParseKey(jwkBytes)
	if parseErr != nil {
		return nil, fmt.Errorf("parse dpop jwk: %w", parseErr)
	}

	thumbprint, thumbErr := publicKey.Thumbprint(crypto.SHA256)
	if thumbErr != nil {
		return nil, fmt.Errorf("dpop jwk thumbprint: %w", thumbErr)
	}
	jkt := base64.RawURLEncoding.EncodeToString(thumbprint)

	var rawPub interface{}
	if rawErr := publicKey.Raw(&rawPub); rawErr != nil {
		return nil, fmt.Errorf("export dpop public key: %w", rawErr)
	}

	verified, verifyErr := jwt.ParseWithClaims(header, jwt.MapClaims{}, func(t *jwt.Token) (interface{}, error) {
		return rawPub, nil
	})
	if verifyErr != nil {
		return nil, fmt.Errorf("verify dpop signature: %w", verifyErr)
	}

	claims, _ := verified.Claims.(jwt.MapClaims)
	htm, _ := claims["htm"].(string)
	if !strings.EqualFold(htm, r.Method) {
		return nil, fmt.Errorf("dpop htm mismatch, got=%s want=%s", htm, r.Method)
	}

	htu, _ := claims["htu"].(string)
	if !sameURL(htu, r) {
		return nil, fmt.Errorf("dpop htu mismatch, got=%s", htu)
	}

	iatF, _ := claims["iat"].(float64)
	iat := int64(iatF)
	now := time.Now().Unix()
	if iat == 0 || iat > now+int64(ClockSkew.Seconds()) || iat < now-int64(ClockSkew.Seconds()) {
		return nil, fmt.Errorf("dpop iat outside window, iat=%d now=%d", iat, now)
	}

	jti, _ := claims["jti"].(string)
	if jti == "" {
		return nil, errors.New("dpop proof missing jti")
	}

	rctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	fresh, storeErr := store.CheckAndStore(rctx, jti, ReplayTTL)
	if storeErr != nil {
		return nil, fmt.Errorf("dpop replay store: %w", storeErr)
	}
	if !fresh {
		return nil, errors.New("dpop jti replay")
	}

	if expectedAccessToken != "" {
		ath, _ := claims["ath"].(string)
		if ath == "" {
			return nil, errors.New("dpop proof missing ath when access token is present")
		}
		expectedHash := sha256.Sum256([]byte(expectedAccessToken))
		expected := base64.RawURLEncoding.EncodeToString(expectedHash[:])
		if ath != expected {
			return nil, errors.New("dpop ath does not match access token")
		}
	}

	return &Proof{JKT: jkt, JTI: jti}, nil
}

func sameURL(htu string, r *http.Request) bool {
	if htu == "" {
		return false
	}
	parsed, err := url.Parse(htu)
	if err != nil {
		return false
	}
	scheme := r.URL.Scheme
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	if parsed.Scheme != scheme {
		return false
	}
	if parsed.Host != r.Host {
		return false
	}
	return parsed.Path == r.URL.Path
}

func TLSCertThumbprint(r *http.Request) string {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return ""
	}
	cert := r.TLS.PeerCertificates[0]
	sum := sha256.Sum256(cert.Raw)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
