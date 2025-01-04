package test

import (
	"fmt"
	"github.com/golang-jwt/jwt"
	"github.com/joho/godotenv"
	"github.com/nimling/samna-auth-middleware/sauth"
	"os"
	"strings"
	"testing"
)

func TestTokenValidation(t *testing.T) {
	_ = godotenv.Load(".env")

	tokenString, ok := os.LookupEnv("TEST_TOKEN_STRING")
	if !ok {
		panic("No clientID specified for AuthMiddleware")
	}

	clientSecret, ok := os.LookupEnv("SAMNA_AUTH_CLIENT_SECRET")
	if !ok {
		panic("No clientSecret specified for AuthMiddleware")
	}

	t.Log("Validating token")
	var tokenClaims sauth.TokenClaims
	// Parse the JWT token
	token, err := jwt.ParseWithClaims(tokenString, &tokenClaims, func(token *jwt.Token) (interface{}, error) {
		t.Log("Validating token with secret")
		// Note: In a production environment, you should validate the signing method
		// and use the appropriate key (symmetric or asymmetric)
		// TODO:: Validate origin here
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		if err := token.Claims.Valid(); err != nil {
			return nil, fmt.Errorf("invalid token: %v", err)
		}

		return []byte(clientSecret), nil // TODO:: Like with microsoft we do not validate the key here...need to figure out a way to do it though
	})

	if token == nil {
		t.Error("Its nil mfa")
		return
	}

	t.Log("Past validation")

	// Check for errors other than signature
	if err != nil {
		if ve, ok := err.(*jwt.ValidationError); ok {
			if ve.Errors&jwt.ValidationErrorExpired != 0 {
				t.Error(err)
			}
		}

		if !strings.Contains(err.Error(), "signature is invalid") { //TODO:: This needs to be fixed
			t.Error(err)
		}

		token.Valid = true
	}

	if tokenClaims.User != nil {
		t.Log(tokenClaims.User)
	}

	return
}
