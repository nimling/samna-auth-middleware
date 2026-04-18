package client

import (
	"os"
)

func VerifyRequest(slug string, secret string) bool {
	expectedSlug := os.Getenv("SAUTH_APP_SLUG")
	expectedSecret := os.Getenv("SAUTH_APP_SECRET")

	if expectedSlug == "" || expectedSecret == "" {
		return false
	}

	return slug == expectedSlug && secret == expectedSecret
}
