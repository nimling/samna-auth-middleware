package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"time"

	"github.com/labstack/echo/v4"
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

func (e *httpClient) Get(ctx echo.Context, url string) (io.ReadCloser, error) {
	return e.execute(ctx, http.MethodGet, http.StatusOK, nil, url)
}

func (e *httpClient) Post(ctx echo.Context, body any, url string) (io.ReadCloser, error) {
	return e.execute(ctx, http.MethodPost, http.StatusCreated, body, url)
}

func (e *httpClient) Put(ctx echo.Context, body any, url string) (io.ReadCloser, error) {
	return e.execute(ctx, http.MethodPut, http.StatusOK, body, url)
}

func (e *httpClient) Delete(ctx echo.Context, url string) (io.ReadCloser, error) {
	return e.execute(ctx, http.MethodDelete, http.StatusOK, nil, url)
}

func (e *httpClient) execute(ctx echo.Context, method string, expectedStatus int, reqBody any, targetURL string) (io.ReadCloser, error) {
	queryParams := ctx.QueryParams()
	if len(queryParams) > 0 {
		parsed, err := neturl.Parse(targetURL)
		if err != nil {
			return nil, fmt.Errorf("parse url for query params: %w", err)
		}
		existing := parsed.Query()
		for k, vs := range queryParams {
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

	req, err := http.NewRequestWithContext(ctx.Request().Context(), method, targetURL, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	accessToken, _ := ctx.Get("access_token").(string)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	req.Header.Set("X-Request-ID", ctx.Response().Header().Get(echo.HeaderXRequestID))
	req.Header.Set("X-User-IP", ctx.RealIP())
	req.Header.Set("SAuth-Client-Slug", e.clientSlug)
	req.Header.Set("SAuth-Client-Secret", e.clientSecret)

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
