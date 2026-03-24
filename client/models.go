package client

import (
	"errors"
	"net/http"
	"net/url"
)

var ErrBadRequest = errors.New("bad request")
var ErrForbidden = errors.New("forbidden")
var ErrNotFound = errors.New("not found")

type SAuthClient struct {
	apiURL string
	client *httpClient
}

type RequestMetadata struct {
	AccessToken string
	RequestID   string
	UserIP      string
	QueryParams url.Values
	DryRun      bool
}

type httpClient struct {
	clientSlug   string
	clientSecret string
	httpClient   *http.Client
}

