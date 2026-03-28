package client

import (
	"os"
)

func newClient() *SAuthClient {
	endpoint := os.Getenv("SAUTH_ENDPOINT")
	slug := os.Getenv("SAUTH_CLIENT_SLUG")
	secret := os.Getenv("SAUTH_CLIENT_SECRET")

	return &SAuthClient{
		apiURL: endpoint + "/sam",
		client: newHTTPClient(slug, secret),
	}
}
