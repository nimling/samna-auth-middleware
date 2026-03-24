package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"time"
)

func DecodeJSON[T any](body io.ReadCloser, err error) (T, error) {
	var zero T
	if err != nil {
		return zero, err
	}
	defer body.Close()
	var result T
	if err := json.NewDecoder(body).Decode(&result); err != nil {
		return zero, fmt.Errorf("decode response: %w", err)
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

func (e *httpClient) Get(ctx context.Context, meta RequestMetadata, url string) (io.ReadCloser, error) {
	return e.execute(ctx, http.MethodGet, http.StatusOK, meta, nil, url)
}

func (e *httpClient) Post(ctx context.Context, meta RequestMetadata, body any, url string) (io.ReadCloser, error) {
	return e.execute(ctx, http.MethodPost, http.StatusCreated, meta, body, url)
}

func (e *httpClient) Put(ctx context.Context, meta RequestMetadata, body any, url string) (io.ReadCloser, error) {
	return e.execute(ctx, http.MethodPut, http.StatusOK, meta, body, url)
}

func (e *httpClient) Delete(ctx context.Context, meta RequestMetadata, url string) (io.ReadCloser, error) {
	return e.execute(ctx, http.MethodDelete, http.StatusOK, meta, nil, url)
}

func (e *httpClient) execute(ctx context.Context, method string, expectedStatus int, meta RequestMetadata, reqBody any, targetURL string) (io.ReadCloser, error) {
	if len(meta.QueryParams) > 0 {
		parsed, err := neturl.Parse(targetURL)
		if err != nil {
			return nil, fmt.Errorf("parse url for query params: %w", err)
		}
		existing := parsed.Query()
		for k, vs := range meta.QueryParams {
			for _, v := range vs {
				existing.Add(k, v)
			}
		}
		parsed.RawQuery = existing.Encode()
		targetURL = parsed.String()
	}

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

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", meta.AccessToken))
	req.Header.Set("X-Request-ID", meta.RequestID)
	req.Header.Set("X-User-IP", meta.UserIP)
	req.Header.Set("SAuth-Client-Slug", e.clientSlug)
	req.Header.Set("SAuth-Client-Secret", e.clientSecret)
	if meta.DryRun {
		req.Header.Set("X-Is-Dry-Run", "true")
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
		return nil, errors.New("unauthorized")

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
