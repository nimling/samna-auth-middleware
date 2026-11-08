package client

import (
	"errors"
	"fmt"
	"net/http"
)

var ErrBadRequest = errors.New("bad request")
var ErrConflict = fmt.Errorf("conflict: %w", ErrBadRequest)
var ErrForbidden = errors.New("forbidden")

// ErrNotHome is a prophet's refusal of a request that belongs on another
// prophet: a write for an app another prophet decides, or a pull by a consumer
// bound to a prophet that has not handed the app over. The error names the
// prophet the request belongs on.
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
