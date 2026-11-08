package echo

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func TestTheHomeAuthBaseIsTheCallbackWithoutCallback(t *testing.T) {
	cases := map[string][]string{
		"https://book.example.test/api/auth": {"https://book.example.test/", "https://book.example.test/api/auth/callback"},
		"http://localhost:4200/api/auth":     {"http://localhost:4200/api/auth/callback/"},
		"":                                   {"https://book.example.test/login"},
	}
	for want, uris := range cases {
		if got := getObjectAssetAuthBase(uris); got != want {
			t.Fatalf("%v: got %q, want %q", uris, got, want)
		}
	}
}

func TestTheRelayCarriesAProphetTokenAndPassesTheHomeAnswerThrough(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	homeID, systemID, assetID := uuid.New(), uuid.New(), uuid.New()
	var seen *http.Request
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Clone(r.Context())
		if r.Header.Get("If-None-Match") == `"h1"` {
			w.Header().Set("ETag", `"h1"`)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("ETag", `"h1"`)
		w.Header().Set("Last-Modified", "Mon, 12 Oct 2026 08:00:00 GMT")
		w.Header().Set("Cache-Control", "private, max-age=60")
		w.Header().Set("Set-Cookie", "home=1")
		w.Write([]byte("pdf-bytes"))
	}))
	defer home.Close()

	token, err := getObjectAssetToken(key, homeID, "https://auth.example.test", "book", systemID, "sauth", time.Now())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	target := home.URL + "/api/auth/asset/" + assetID.String() + "/data"

	e := echo.New()
	rec := httptest.NewRecorder()
	ctx := e.NewContext(httptest.NewRequest(http.MethodGet, "/sam/object_asset/x/data", nil), rec)
	if err := getRelayedAssetData(ctx, target, token); err != nil {
		t.Fatalf("relay: %v", err)
	}
	if seen.URL.Path != "/api/auth/asset/"+assetID.String()+"/data" {
		t.Fatalf("the relay asks the home for the asset by its id, got %s", seen.URL.Path)
	}
	raw, ok := strings.CutPrefix(seen.Header.Get("Authorization"), "Bearer ")
	if !ok {
		t.Fatal("the relay carries a bearer token")
	}
	var claims objectAssetToken
	parsed, err := jwt.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) { return &key.PublicKey, nil }, jwt.WithExpirationRequired())
	if err != nil || !parsed.Valid {
		t.Fatalf("the relay token verifies against the home app key: %v", err)
	}
	if parsed.Header["kid"] != homeID.String() || claims.Issuer != "https://auth.example.test" || claims.ClientID != "sauth" ||
		claims.Subject != systemID.String() || len(claims.Audience) != 1 || claims.Audience[0] != "book" {
		t.Fatalf("the token names the home key, the prophet and the home app, got %v %+v", parsed.Header, claims)
	}
	if claims.ExpiresAt.Sub(claims.IssuedAt.Time) > time.Minute {
		t.Fatalf("the relay token is short lived, got %v", claims.ExpiresAt.Sub(claims.IssuedAt.Time))
	}
	if rec.Code != http.StatusOK || rec.Body.String() != "pdf-bytes" {
		t.Fatalf("the home's 200 and body pass through, got %d %q", rec.Code, rec.Body.String())
	}
	for name, want := range map[string]string{"Content-Type": "application/pdf", "ETag": `"h1"`, "Last-Modified": "Mon, 12 Oct 2026 08:00:00 GMT", "Cache-Control": "private, max-age=60"} {
		if got := rec.Header().Get(name); got != want {
			t.Fatalf("%s passes through unchanged, got %q want %q", name, got, want)
		}
	}
	if rec.Header().Get("Set-Cookie") != "" {
		t.Fatal("only the pass-through headers leave the prophet")
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/sam/object_asset/x/data", nil)
	req.Header.Set("If-None-Match", `"h1"`)
	req.Header.Set("If-Modified-Since", "Mon, 12 Oct 2026 08:00:00 GMT")
	ctx = e.NewContext(req, rec)
	if err := getRelayedAssetData(ctx, target, token); err != nil {
		t.Fatalf("relay conditional: %v", err)
	}
	if seen.Header.Get("If-None-Match") != `"h1"` || seen.Header.Get("If-Modified-Since") != "Mon, 12 Oct 2026 08:00:00 GMT" {
		t.Fatalf("the conditional headers reach the home, got %v", seen.Header)
	}
	if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 || rec.Header().Get("ETag") != `"h1"` {
		t.Fatalf("the home's 304 passes through without a body, got %d %q", rec.Code, rec.Body.String())
	}
}
