package test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/joho/godotenv"

	"github.com/nimling/samna-auth-middleware/middleware"
)

func TestTokenValidation(t *testing.T) {
	_ = godotenv.Load("../.env")

	jwksURL, ok := os.LookupEnv("SAUTH_JWKS_URL")
	if !ok {
		t.Fatal("No jwksURL specified for AuthMiddleware")
	}
	tokenString, ok := os.LookupEnv("TEST_TOKEN_STRING")
	if !ok {
		t.Fatal("No clientID specified for AuthMiddleware")
	}
	expectedIss, ok := os.LookupEnv("TEST_TOKEN_ISS")
	if !ok {
		t.Fatal("No expected token issuer specified for AuthMiddleware")
	}

	jwksClient, err := keyfunc.NewDefault([]string{jwksURL})
	if err != nil {
		t.Fatalf("JWK client initialization failed: %v", err)
	}

	claims, err := middleware.ValidateToken(tokenString, expectedIss, jwksClient.Keyfunc)
	if err != nil {
		t.Fatalf("Error validating token: %v", err)
	}

	b, _ := json.MarshalIndent(claims, "", "  ")
	fmt.Println(string(b))
}
