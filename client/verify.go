package client

import (
	"os"
)

func VerifyRequest(slug string, secret string) bool {
	expectedSlug := os.Getenv("SAUTH_CLIENT_SLUG")
	expectedSecret := os.Getenv("SAUTH_CLIENT_SECRET")

	if expectedSlug == "" || expectedSecret == "" {
		return false
	}

	return slug == expectedSlug && secret == expectedSecret
}
