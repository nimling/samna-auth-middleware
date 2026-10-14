package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/nimling/samna-auth-middleware/v2/utils"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
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
	Data  json.RawMessage `json:"data"`
	Error *ResponseError  `json:"error"`
}

type ResponseError struct {
	Message string `json:"message"`
}

type forwardKey struct{}

type Forward struct {
	IP        string
	UserAgent string
	UserID    string
}

func WithForward(ctx context.Context, f Forward) context.Context {
	return context.WithValue(ctx, forwardKey{}, f)
}

type contentTypeKey struct{}

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

type ClientCredentials struct {
	ID     string
	Secret string
}

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

const HeaderSyncSeq = "SAuth-Seq"

type SyncCursor struct {
	Txid int64 `bun:"txid" json:"txid"`
	Seq  int64 `bun:"seq" json:"seq"`
}

func (c SyncCursor) Less(o SyncCursor) bool {
	return c.Txid < o.Txid || (c.Txid == o.Txid && c.Seq < o.Seq)
}

func (c SyncCursor) IsZero() bool { return c.Txid == 0 && c.Seq == 0 }

func (c SyncCursor) String() string {
	return strconv.FormatInt(c.Txid, 10) + "." + strconv.FormatInt(c.Seq, 10)
}

func ParseSyncCursor(s string) (SyncCursor, error) {
	txid, seq, ok := strings.Cut(s, ".")
	if !ok {
		return SyncCursor{}, fmt.Errorf("sync cursor %q: want <txid>.<seq>", s)
	}
	t, err := strconv.ParseInt(txid, 10, 64)
	if err != nil || t < 0 {
		return SyncCursor{}, fmt.Errorf("sync cursor %q: bad txid", s)
	}
	q, err := strconv.ParseInt(seq, 10, 64)
	if err != nil || q < 0 {
		return SyncCursor{}, fmt.Errorf("sync cursor %q: bad seq", s)
	}
	return SyncCursor{Txid: t, Seq: q}, nil
}

const HeaderProphet = "SAuth-Prophet"

const HeaderHome = "SAuth-Home"

const HeaderPeerSecret = "SAuth-Peer-Secret"

type prophetKey struct{}

func WithProphet(ctx context.Context, prophet *string) context.Context {
	return context.WithValue(ctx, prophetKey{}, prophet)
}

type targetKey struct{}

type Target struct {
	Endpoint   string
	Slug       string
	Secret     string
	PeerSecret string
}

func WithTarget(ctx context.Context, t Target) context.Context {
	return context.WithValue(ctx, targetKey{}, t)
}

type syncSeqKey struct{}

func WithSyncSeq(ctx context.Context, seq *SyncCursor) context.Context {
	return context.WithValue(ctx, syncSeqKey{}, seq)
}

type requestSeqKey struct{}

type RequestSeq struct {
	mu  sync.Mutex
	seq SyncCursor
}

func WithRequestSeq(ctx context.Context) (context.Context, *RequestSeq, bool) {
	if r, ok := ctx.Value(requestSeqKey{}).(*RequestSeq); ok {
		return ctx, r, false
	}
	r := &RequestSeq{}
	return context.WithValue(ctx, requestSeqKey{}, r), r, true
}

func (r *RequestSeq) Get() SyncCursor {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seq
}

func (r *RequestSeq) put(v SyncCursor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seq.Less(v) {
		r.seq = v
	}
}

type statusKey struct{}

func WithStatus(ctx context.Context, status *int) context.Context {
	return context.WithValue(ctx, statusKey{}, status)
}

type timeoutKey struct{}

func WithTimeout(ctx context.Context, d time.Duration) context.Context {
	return context.WithValue(ctx, timeoutKey{}, d)
}

func timeoutFrom(ctx context.Context) (time.Duration, bool) {
	d, ok := ctx.Value(timeoutKey{}).(time.Duration)
	return d, ok && d > 0
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
	hc := e.httpClient
	if d, ok := timeoutFrom(ctx); ok {
		hc = &http.Client{Timeout: d, Transport: hc.Transport}
	}
	resp, err := e.send(ctx, hc, method, reqBody, targetURL, nil)
	if err != nil {
		return nil, err
	}
	return responseBody(resp)
}

func (e *httpClient) send(ctx context.Context, hc *http.Client, method string, reqBody any, targetURL string, header http.Header) (*http.Response, error) {
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
	if t, ok := ctx.Value(targetKey{}).(Target); ok && t.PeerSecret != "" {
		req.Header.Set(HeaderPeerSecret, t.PeerSecret)
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

	for key, values := range header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	if reqBody != nil {
		req.Header.Set("Content-Type", contentTypeFrom(ctx))
	}

	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	if seq, ok := ctx.Value(syncSeqKey{}).(*SyncCursor); ok && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if v, err := ParseSyncCursor(resp.Header.Get(HeaderSyncSeq)); err == nil {
			if seq.Less(v) {
				*seq = v
			}
			if r, ok := ctx.Value(requestSeqKey{}).(*RequestSeq); ok {
				r.put(v)
			}
		}
	}
	if status, ok := ctx.Value(statusKey{}).(*int); ok {
		*status = resp.StatusCode
	}
	if prophet, ok := ctx.Value(prophetKey{}).(*string); ok && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		*prophet = resp.Header.Get(HeaderProphet)
	}
	return resp, nil
}

func responseBody(resp *http.Response) (io.ReadCloser, error) {
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return resp.Body, nil
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, ErrUnauthorized
	case resp.StatusCode == http.StatusForbidden:
		return nil, ErrForbidden
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode == http.StatusConflict && resp.Header.Get(HeaderHome) != "":
		resp.Body.Close()
		return nil, fmt.Errorf("%w, the prophet is %s", ErrNotHome, resp.Header.Get(HeaderHome))
	case resp.StatusCode == http.StatusConflict:
		return nil, ErrConflict
	case resp.StatusCode == http.StatusBadRequest:
		return nil, refusal(resp.Body, ErrBadRequest)
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
	c := newClient(ctx)
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

func GetStream(ctx context.Context, path string, header http.Header) (*http.Response, error) {
	c := newClient(ctx)
	u, err := joinURL(c.apiURL, path)
	if err != nil {
		return nil, fmt.Errorf("build url: %w", err)
	}
	resp, err := c.client.send(ctx, &http.Client{Transport: c.client.httpClient.Transport}, http.MethodGet, nil, u, header)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotModified || (resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return resp, nil
	}
	defer resp.Body.Close()
	_, err = responseBody(resp)
	return nil, err
}

func Post(ctx context.Context, path string, payload any, out any) error {
	c := newClient(ctx)
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
	c := newClient(ctx)
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
	c := newClient(ctx)
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
	c := newClient(ctx)
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

func refusal(body io.ReadCloser, sentinel error) error {
	defer body.Close()
	var envelope ResponseEnvelope
	if err := json.NewDecoder(body).Decode(&envelope); err != nil || envelope.Error == nil || envelope.Error.Message == "" {
		return sentinel
	}
	return fmt.Errorf("%w: %s", sentinel, envelope.Error.Message)
}

var AssetHeaders = []string{"Content-Type", "Content-Length", "ETag", "Last-Modified", "Cache-Control"}

var ConditionalHeaders = []string{"If-None-Match", "If-Modified-Since"}
