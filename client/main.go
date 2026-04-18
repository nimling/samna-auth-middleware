package client

import (
	"os"
)

func newClient() *SAuthClient {
	endpoint := os.Getenv("SAUTH_ENDPOINT")
	slug := os.Getenv("SAUTH_APP_SLUG")
	secret := os.Getenv("SAUTH_APP_SECRET")

	return &SAuthClient{
		apiURL: endpoint + "/sam",
		client: newHTTPClient(slug, secret),
	}
}
