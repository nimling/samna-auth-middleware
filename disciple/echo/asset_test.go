package echo

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/v2/client"
	"github.com/nimling/samna-auth-middleware/v2/utils"
)

const (
	assetTestSlug    = "assetapp"
	assetTestProphet = "sauth"
	assetTestIssuer  = "https://auth.example.test"
)

type assetTestClaims struct {
	jwt.RegisteredClaims
	ClientID string `json:"client_id"`
}

func getAssetTestKeys(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	kid := uuid.NewString()
	jwks, _ := json.Marshal(map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": kid,
		"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(jwks)
	}))
	t.Cleanup(server.Close)
	jwksURL, _ := url.Parse(server.URL)
	utils.SetOIDC(assetTestIssuer, url.URL{}, *jwksURL)
	t.Setenv("SAUTH_APP_SLUG", assetTestSlug)
	client.Bind("", assetTestProphet)
	t.Cleanup(func() { client.Bind("", "") })
	return key, kid
}

func getAssetTestToken(t *testing.T, key *rsa.PrivateKey, kid, audience, clientID string) string {
	t.Helper()
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, assetTestClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    assetTestIssuer,
			Subject:   uuid.NewString(),
			Audience:  jwt.ClaimStrings{audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
		},
		ClientID: clientID,
	})
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

func getAssetTestRequest(t *testing.T, assetID uuid.UUID, token string, header map[string]string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/asset/"+assetID.String()+"/data", nil)
	if token != "" {
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("id")
	ctx.SetParamValues(assetID.String())
	return ctx, rec
}

func TestTheInboundAssetRouteAcceptsOnlyTheProphetToken(t *testing.T) {
	key, kid := getAssetTestKeys(t)
	assetID := uuid.New()
	var opened []uuid.UUID
	source := func(_ context.Context, id uuid.UUID) (*AssetResponse, error) {
		opened = append(opened, id)
		return &AssetResponse{
			Status: http.StatusOK,
			Header: http.Header{"Content-Type": {"image/png"}, "Etag": {`"v1"`}, "Cache-Control": {"max-age=60"}, "X-Internal": {"secret"}},
			Body:   io.NopCloser(strings.NewReader("png-bytes")),
		}, nil
	}

	ctx, rec := getAssetTestRequest(t, assetID, getAssetTestToken(t, key, kid, assetTestSlug, assetTestProphet), nil)
	if err := getAssetData(ctx, source); err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != "png-bytes" {
		t.Fatalf("the prophet's token opens the asset, got %d %q", rec.Code, rec.Body.String())
	}
	if len(opened) != 1 || opened[0] != assetID {
		t.Fatalf("the host source opens the asset by its id, got %v", opened)
	}
	if rec.Header().Get("ETag") != `"v1"` || rec.Header().Get("Cache-Control") != "max-age=60" || rec.Header().Get("X-Internal") != "" {
		t.Fatalf("only the pass-through headers leave, got %v", rec.Header())
	}

	refused := map[string]string{
		"a user token issued to the app":   getAssetTestToken(t, key, kid, assetTestSlug, assetTestSlug),
		"a prophet token for another app":  getAssetTestToken(t, key, kid, "other", assetTestProphet),
		"no token":                         "",
		"a token signed by an unknown key": getAssetTestToken(t, getMustKey(t), kid, assetTestSlug, assetTestProphet),
	}
	for name, token := range refused {
		ctx, rec := getAssetTestRequest(t, assetID, token, nil)
		if err := getAssetData(ctx, source); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s must be refused, got %d", name, rec.Code)
		}
	}
	if len(opened) != 1 {
		t.Fatalf("a refused request never reaches the source, opened %d times", len(opened))
	}

	ctx, rec = getAssetTestRequest(t, assetID, getAssetTestToken(t, key, kid, assetTestSlug, assetTestProphet), map[string]string{"If-None-Match": `"v1"`})
	if err := getAssetData(ctx, source); err != nil {
		t.Fatalf("conditional: %v", err)
	}
	if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("a matching If-None-Match answers 304 without a body, got %d %q", rec.Code, rec.Body.String())
	}

	ctx, rec = getAssetTestRequest(t, assetID, getAssetTestToken(t, key, kid, assetTestSlug, assetTestProphet), nil)
	if err := getAssetData(ctx, nil); err != nil {
		t.Fatalf("no source: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a host without a source answers 404, got %d", rec.Code)
	}
}

func getMustKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	return key
}

type assetTestProphetCall struct {
	path   string
	header http.Header
}

func getAssetTestProphet(t *testing.T, answer func(w http.ResponseWriter, r *http.Request)) *[]assetTestProphetCall {
	t.Helper()
	var mu sync.Mutex
	calls := make([]assetTestProphetCall, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, assetTestProphetCall{path: r.URL.Path, header: r.Header.Clone()})
		mu.Unlock()
		answer(w, r)
	}))
	t.Cleanup(server.Close)
	t.Setenv("SAUTH_ENDPOINT", server.URL)
	t.Setenv("SAUTH_APP_SLUG", assetTestSlug)
	t.Setenv("SAUTH_APP_SECRET", "secret")
	client.Bind("", "")
	return &calls
}

func getAssetTestReader(header map[string]string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/asset/x/data", nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

type assetTestCache struct {
	mu      sync.Mutex
	stored  *AssetResponse
	body    []byte
	gets    []http.Header
	puts    int
	putBody []byte
	putHead http.Header
}

func (c *assetTestCache) Get(_ context.Context, _ uuid.UUID, conditional http.Header) (*AssetResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gets = append(c.gets, conditional)
	if c.stored == nil {
		return nil, nil
	}
	return &AssetResponse{Status: c.stored.Status, Header: c.stored.Header.Clone(), Body: io.NopCloser(bytes.NewReader(c.body))}, nil
}

func (c *assetTestCache) Put(_ context.Context, _ uuid.UUID, header http.Header, body io.Reader) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.puts++
	c.putBody = data
	c.putHead = header
	return nil
}

func TestTheOutboundAssetRoutePassesTheProphetsAnswerThrough(t *testing.T) {
	linkID := uuid.New()
	calls := getAssetTestProphet(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v2"` {
			w.Header().Set("ETag", `"v2"`)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("ETag", `"v2"`)
		w.Header().Set("Last-Modified", "Tue, 13 Oct 2026 10:00:00 GMT")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.Header().Set("X-Prophet", "internal")
		w.Write([]byte("jpeg-bytes"))
	})

	ctx, rec := getAssetTestReader(nil)
	if err := getProphetAssetData(ctx, linkID, nil); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != "jpeg-bytes" {
		t.Fatalf("the prophet's 200 and body pass through, got %d %q", rec.Code, rec.Body.String())
	}
	for name, want := range map[string]string{"Content-Type": "image/jpeg", "ETag": `"v2"`, "Last-Modified": "Tue, 13 Oct 2026 10:00:00 GMT", "Cache-Control": "public, max-age=300", "Content-Length": "10"} {
		if got := rec.Header().Get(name); got != want {
			t.Fatalf("%s passes through unchanged, got %q want %q", name, got, want)
		}
	}
	if rec.Header().Get("X-Prophet") != "" {
		t.Fatalf("only the pass-through headers leave, got %v", rec.Header())
	}
	if (*calls)[0].path != "/sam/object_asset/"+linkID.String()+"/data" || (*calls)[0].header.Get("SAuth-App-Slug") != assetTestSlug {
		t.Fatalf("the prophet is called on the link with the app credentials, got %+v", (*calls)[0])
	}

	ctx, rec = getAssetTestReader(map[string]string{"If-None-Match": `"v2"`, "If-Modified-Since": "Tue, 13 Oct 2026 10:00:00 GMT"})
	if err := getProphetAssetData(ctx, linkID, nil); err != nil {
		t.Fatalf("conditional: %v", err)
	}
	last := (*calls)[len(*calls)-1]
	if last.header.Get("If-None-Match") != `"v2"` || last.header.Get("If-Modified-Since") != "Tue, 13 Oct 2026 10:00:00 GMT" {
		t.Fatalf("the reader's conditional headers are forwarded, got %v", last.header)
	}
	if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 || rec.Header().Get("ETag") != `"v2"` {
		t.Fatalf("a 304 passes through without a body, got %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
}

func TestTheOutboundAssetRouteUsesTheHostCacheOnlyWhileFresh(t *testing.T) {
	linkID := uuid.New()
	status := http.StatusOK
	cacheControl := "public, max-age=300"
	calls := getAssetTestProphet(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v3"`)
		w.Header().Set("Cache-Control", cacheControl)
		if status == http.StatusNotModified && r.Header.Get("If-None-Match") == `"v3"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("fresh-bytes"))
	})

	cache := &assetTestCache{}
	ctx, rec := getAssetTestReader(nil)
	if err := getProphetAssetData(ctx, linkID, cache); err != nil {
		t.Fatalf("miss: %v", err)
	}
	if rec.Body.String() != "fresh-bytes" || cache.puts != 1 || string(cache.putBody) != "fresh-bytes" || cache.putHead.Get("ETag") != `"v3"` {
		t.Fatalf("a miss streams the prophet's 200 and puts it with its headers, got %q puts=%d %q", rec.Body.String(), cache.puts, cache.putBody)
	}

	cache.stored = &AssetResponse{Status: http.StatusOK, Header: http.Header{
		"Content-Type": {"image/png"}, "Etag": {`"v3"`}, "Cache-Control": {"max-age=300"}, "Date": {time.Now().UTC().Format(http.TimeFormat)},
	}}
	cache.body = []byte("cached-bytes")
	before := len(*calls)
	ctx, rec = getAssetTestReader(nil)
	if err := getProphetAssetData(ctx, linkID, cache); err != nil {
		t.Fatalf("fresh: %v", err)
	}
	if rec.Body.String() != "cached-bytes" || len(*calls) != before {
		t.Fatalf("a fresh stored answer is served without asking the prophet, got %q and %d calls", rec.Body.String(), len(*calls)-before)
	}

	cache.stored.Header.Set("Date", time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat))
	status = http.StatusNotModified
	ctx, rec = getAssetTestReader(nil)
	if err := getProphetAssetData(ctx, linkID, cache); err != nil {
		t.Fatalf("stale: %v", err)
	}
	last := (*calls)[len(*calls)-1]
	if len(*calls) != before+1 || last.header.Get("If-None-Match") != `"v3"` {
		t.Fatalf("a stale stored answer is revalidated with its validators, got %v", last.header)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != "cached-bytes" || cache.puts != 1 {
		t.Fatalf("a 304 on revalidation serves the stored body and puts nothing, got %d %q puts=%d", rec.Code, rec.Body.String(), cache.puts)
	}

	status = http.StatusOK
	ctx, rec = getAssetTestReader(nil)
	if err := getProphetAssetData(ctx, linkID, cache); err != nil {
		t.Fatalf("changed: %v", err)
	}
	if rec.Body.String() != "fresh-bytes" || cache.puts != 2 {
		t.Fatalf("a 200 on revalidation streams the new body and puts it, got %q puts=%d", rec.Body.String(), cache.puts)
	}

	cache.stored.Header.Set("Cache-Control", "private, max-age=300")
	cache.stored.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	cacheControl = "no-store"
	before = len(*calls)
	ctx, rec = getAssetTestReader(nil)
	if err := getProphetAssetData(ctx, linkID, cache); err != nil {
		t.Fatalf("no-store: %v", err)
	}
	if len(*calls) != before+1 {
		t.Fatal("a private stored answer is never served without the prophet")
	}
	if rec.Body.String() != "fresh-bytes" || cache.puts != 2 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("a no-store answer is streamed with its header and never put, got %q puts=%d", rec.Body.String(), cache.puts)
	}
}
