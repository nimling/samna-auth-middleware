package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type ResponseEnvelope struct {
	Data json.RawMessage `json:"data"`
}

type accessClaimKey struct{}

func WithAccessClaim(ctx context.Context, claim string) context.Context {
	if claim == "" {
		return ctx
	}
	return context.WithValue(ctx, accessClaimKey{}, claim)
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
		httpClient:   &http.Client{Timeout: time.Second * 5},
	}
}

func (e *httpClient) Get(ctx context.Context, url string) (io.ReadCloser, error) {
	return e.execute(ctx, http.MethodGet, http.StatusOK, nil, url)
}

func (e *httpClient) Post(ctx context.Context, body any, url string) (io.ReadCloser, error) {
	return e.execute(ctx, http.MethodPost, http.StatusCreated, body, url)
}

func (e *httpClient) Put(ctx context.Context, body any, url string) (io.ReadCloser, error) {
	return e.execute(ctx, http.MethodPut, http.StatusOK, body, url)
}

func (e *httpClient) Patch(ctx context.Context, body any, url string) (io.ReadCloser, error) {
	return e.execute(ctx, http.MethodPatch, http.StatusOK, body, url)
}

func (e *httpClient) Delete(ctx context.Context, url string) (io.ReadCloser, error) {
	return e.execute(ctx, http.MethodDelete, http.StatusOK, nil, url)
}

func (e *httpClient) execute(ctx context.Context, method string, expectedStatus int, reqBody any, targetURL string) (io.ReadCloser, error) {
	var body io.Reader
	if reqBody != nil {
		payloadBytes, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		body = bytes.NewReader(payloadBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("X-Request-ID", generateRequestID())
	req.Header.Set("X-Client-IP", localIP())
	req.Header.Set("SAuth-App-Slug", e.clientSlug)
	req.Header.Set("SAuth-App-Secret", e.clientSecret)

	if claim, ok := ctx.Value(accessClaimKey{}).(string); ok && claim != "" {
		req.Header.Set("SAuth-Access-Claim", claim)
	}

	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	switch {
	case resp.StatusCode == expectedStatus:
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

func Delete(ctx context.Context, path string, out any) error {
	c := newClient()
	u, err := joinURL(c.apiURL, path)
	if err != nil {
		return fmt.Errorf("build url: %w", err)
	}
	body, err := c.client.Delete(ctx, u)
	if err != nil {
		return err
	}
	return decodeInto(body, out)
}
