package client

import (
	"errors"
	"fmt"
	"net/http"
)

var ErrBadRequest = errors.New("bad request")
var ErrConflict = fmt.Errorf("conflict: %w", ErrBadRequest)
var ErrForbidden = errors.New("forbidden")

var ErrNotHome = fmt.Errorf("not the home prophet: %w", ErrConflict)
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
