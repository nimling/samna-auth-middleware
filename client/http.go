package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/nimling/samna-auth-middleware/utils"
	"io"
	"net/http"
	"strconv"
	"time"
)

func requestTimeout() time.Duration {
	if v := utils.Cfg().ClientTimeout; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 5 * time.Second
}

type ResponseEnvelope struct {
	Data json.RawMessage `json:"data"`
}

type forwardKey struct{}

// Forward carries the browser identity of the visitor an app is acting for, so
// the prophet records the visitor rather than the calling app. Every request
// built from a context holding one sends it on the SAuth-Forwarded headers.
type Forward struct {
	IP        string
	UserAgent string
	UserID    string
}

// WithForward attaches the visitor identity to a context handed to Get, Post,
// Put, Patch or Delete.
func WithForward(ctx context.Context, f Forward) context.Context {
	return context.WithValue(ctx, forwardKey{}, f)
}

type contentTypeKey struct{}

// WithContentType overrides the request content type for a body handed to Post,
// Put or Patch as raw bytes, for payloads that are not json.
func WithContentType(ctx context.Context, contentType string) context.Context {
	return context.WithValue(ctx, contentTypeKey{}, contentType)
}

func contentTypeFrom(ctx context.Context) string {
	if v, ok := ctx.Value(contentTypeKey{}).(string); ok && v != "" {
		return v
	}
	return "application/json"
}

type clientCredentialsKey struct{}

// ClientCredentials names the machine client a request acts as, for routes the
// prophet gates on a client actor rather than the app itself.
type ClientCredentials struct {
	ID     string
	Secret string
}

// WithClientCredentials attaches a machine client identity to a context handed
// to Get, Post, Put, Patch or Delete.
func WithClientCredentials(ctx context.Context, c ClientCredentials) context.Context {
	return context.WithValue(ctx, clientCredentialsKey{}, c)
}

func clientCredentialsFrom(ctx context.Context) (ClientCredentials, bool) {
	c, ok := ctx.Value(clientCredentialsKey{}).(ClientCredentials)
	return c, ok && c.ID != "" && c.Secret != ""
}

func forwardFrom(ctx context.Context) (Forward, bool) {
	f, ok := ctx.Value(forwardKey{}).(Forward)
	return f, ok
}

func DecodeJSON[T any](body io.ReadCloser, err error) (T, error) {
	var zero T
	if err != nil {
		return zero, err
	}
	defer body.Close()

	var envelope ResponseEnvelope
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		return zero, fmt.Errorf("decode envelope: %w", err)
	}

	var result T
	if err := json.Unmarshal(envelope.Data, &result); err != nil {
		return zero, fmt.Errorf("decode data: %w", err)
	}
	return result, nil
}

func newHTTPClient(clientSlug, clientSecret string) *httpClient {
	return &httpClient{
		clientSlug:   clientSlug,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: requestTimeout()},
	}
}

func (e *httpClient) Get(ctx context.Context, url string) (io.ReadCloser, error) {
	return e.execute(ctx, http.MethodGet, nil, url)
}

func (e *httpClient) Post(ctx context.Context, body any, url string) (io.ReadCloser, error) {
	return e.execute(ctx, http.MethodPost, body, url)
}

func (e *httpClient) Put(ctx context.Context, body any, url string) (io.ReadCloser, error) {
	return e.execute(ctx, http.MethodPut, body, url)
}

func (e *httpClient) Patch(ctx context.Context, body any, url string) (io.ReadCloser, error) {
	return e.execute(ctx, http.MethodPatch, body, url)
}

func (e *httpClient) Delete(ctx context.Context, body any, url string) (io.ReadCloser, error) {
	return e.execute(ctx, http.MethodDelete, body, url)
}

func (e *httpClient) execute(ctx context.Context, method string, reqBody any, targetURL string) (io.ReadCloser, error) {
	var body io.Reader
	if reqBody != nil {
		if raw, ok := reqBody.([]byte); ok {
			body = bytes.NewReader(raw)
		} else {
			payloadBytes, err := json.Marshal(reqBody)
			if err != nil {
				return nil, fmt.Errorf("marshal request body: %w", err)
			}
			body = bytes.NewReader(payloadBytes)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("X-Request-ID", generateRequestID())
	req.Header.Set("X-Client-IP", localIP())
	req.Header.Set("SAuth-App-Slug", e.clientSlug)
	if c, ok := clientCredentialsFrom(ctx); ok {
		req.SetBasicAuth(c.ID, c.Secret)
	} else {
		req.Header.Set("SAuth-App-Secret", e.clientSecret)
	}

	if f, ok := forwardFrom(ctx); ok {
		if f.IP != "" {
			req.Header.Set("SAuth-Forwarded-For", f.IP)
		}
		if f.UserAgent != "" {
			req.Header.Set("SAuth-Forwarded-Agent", f.UserAgent)
		}
		if f.UserID != "" {
			req.Header.Set("SAuth-Forwarded-User", f.UserID)
		}
	}

	if reqBody != nil {
		req.Header.Set("Content-Type", contentTypeFrom(ctx))
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return resp.Body, nil
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, ErrUnauthorized
	case resp.StatusCode == http.StatusForbidden:
		return nil, ErrForbidden
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusConflict:
		return nil, ErrBadRequest
	default:
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
}

func decodeInto(body io.ReadCloser, out any) error {
	defer body.Close()
	if out == nil {
		return nil
	}
	var envelope ResponseEnvelope
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		return fmt.Errorf("decode envelope: %w", err)
	}
	if len(envelope.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("decode data: %w", err)
	}
	return nil
}

func Get(ctx context.Context, path string, out any) error {
	c := newClient()
	u, err := joinURL(c.apiURL, path)
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}
	body, err := c.client.Get(ctx, u)
	if err != nil {
		return err
	}
	return decodeInto(body, out)
}

func Post(ctx context.Context, path string, payload any, out any) error {
	c := newClient()
	u, err := joinURL(c.apiURL, path)
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}
	body, err := c.client.Post(ctx, payload, u)
	if err != nil {
		return err
	}
	return decodeInto(body, out)
}

func Put(ctx context.Context, path string, payload any, out any) error {
	c := newClient()
	u, err := joinURL(c.apiURL, path)
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}
	body, err := c.client.Put(ctx, payload, u)
	if err != nil {
		return err
	}
	return decodeInto(body, out)
}

func Patch(ctx context.Context, path string, payload any, out any) error {
	c := newClient()
	u, err := joinURL(c.apiURL, path)
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}
	body, err := c.client.Patch(ctx, payload, u)
	if err != nil {
		return err
	}
	return decodeInto(body, out)
}

func Delete(ctx context.Context, path string, payload any, out any) error {
	c := newClient()
	u, err := joinURL(c.apiURL, path)
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}
	body, err := c.client.Delete(ctx, payload, u)
	if err != nil {
		return err
	}
	return decodeInto(body, out)
}
