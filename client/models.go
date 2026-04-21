package client

import (
	"errors"
	"net/http"
)

var ErrBadRequest = errors.New("bad request")
var ErrForbidden = errors.New("forbidden")
var ErrNotFound = errors.New("not found")
var ErrUnauthorized = errors.New("unauthorized")

type SAuthClient struct {
	apiURL string
	client *httpClient
}

type httpClient struct {
	clientSlug   string
	clientSecret string
	httpClient   *http.Client
}
