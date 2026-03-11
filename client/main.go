package client

import (
	"net/url"
	"os"
	"path"
)

func newClient() *SAuthClient {
	endpoint := os.Getenv("SAMNA_AUTH_ENDPOINT")
	slug := os.Getenv("SAMNA_AUTH_CLIENT_SLUG")
	secret := os.Getenv("SAMNA_AUTH_CLIENT_SECRET")

	return &SAuthClient{
		apiURL: endpoint + "/sam",
		client: newHTTPClient(slug, secret),
	}
}

func joinURL(baseURL string, segments ...string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	allSegments := append([]string{base.Path}, segments...)
	base.Path = path.Join(allSegments...)

	return base.String(), nil
}

