package bff_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/nimling/samna-auth-middleware/bff"
)

type forwardCapture struct {
	method      string
	path        string
	contentType string
	body        map[string]string
}

func prophetStub(t *testing.T) *forwardCapture {
	t.Helper()
	capture := &forwardCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.method = r.Method
		capture.path = r.URL.Path
		capture.contentType = r.Header.Get("Content-Type")
		capture.body = map[string]string{}
		if err := json.NewDecoder(r.Body).Decode(&capture.body); err != nil {
			t.Errorf("decode forward body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SAUTH_ENDPOINT", srv.URL)
	t.Setenv("SAUTH_APP_SLUG", "docs")
	t.Setenv("SAUTH_APP_SECRET", "secret")
	return capture
}

func subscribeContext(t *testing.T, method string, body string) (echo.Context, *httptest.ResponseRecorder) {
	t.Helper()
	t.Setenv("SAUTH_APP_SLUG", "docs")
	e := echo.New()
	req := httptest.NewRequest(method, "/api/sauth/app/docs/subscription/subscribe", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	ctx := e.NewContext(req, rec)
	ctx.SetParamNames("app_slug")
	ctx.SetParamValues("docs")
	return ctx, rec
}

func TestPostSubscriptionForwardsTheFeedToTheProphet(t *testing.T) {
	capture := prophetStub(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rss.xml"), []byte("<rss/>"), 0o644); err != nil {
		t.Fatalf("write feed: %v", err)
	}

	ctx, rec := subscribeContext(t, http.MethodPost, `{"email":"reader@example.com","feed":"https://site.example/rss.xml","name":"news"}`)
	if err := bff.PostSubscription(bff.Config{StaticDir: dir})(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if capture.method != http.MethodPost {
		t.Fatalf("forward method = %q, want POST", capture.method)
	}
	if capture.path != "/api/app/docs/subscription/subscribe" {
		t.Fatalf("forward path = %q", capture.path)
	}
	if capture.contentType != "application/json" {
		t.Fatalf("forward content type = %q, want application/json", capture.contentType)
	}
	if capture.body["email"] != "reader@example.com" {
		t.Fatalf("forward email = %q", capture.body["email"])
	}
	if capture.body["feed"] != "https://site.example/rss.xml" {
		t.Fatalf("forward feed = %q", capture.body["feed"])
	}
	if capture.body["name"] != "news" {
		t.Fatalf("forward name = %q", capture.body["name"])
	}
	if capture.body["rss"] != "<rss/>" {
		t.Fatalf("forward rss = %q, want the feed document", capture.body["rss"])
	}
}

func TestDeleteSubscriptionForwardsTheRemoval(t *testing.T) {
	capture := prophetStub(t)

	ctx, rec := subscribeContext(t, http.MethodDelete, `{"email":"reader@example.com","name":"news"}`)
	if err := bff.DeleteSubscription(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if capture.method != http.MethodDelete {
		t.Fatalf("forward method = %q, want DELETE", capture.method)
	}
	if capture.path != "/api/app/docs/subscription/subscribe" {
		t.Fatalf("forward path = %q", capture.path)
	}
	if capture.body["email"] != "reader@example.com" {
		t.Fatalf("forward email = %q", capture.body["email"])
	}
	if capture.body["name"] != "news" {
		t.Fatalf("forward name = %q", capture.body["name"])
	}
	if _, ok := capture.body["rss"]; ok {
		t.Fatalf("removal forward carries an rss field")
	}
}

func TestPostSubscriptionRejectsAnInvalidEmail(t *testing.T) {
	ctx, rec := subscribeContext(t, http.MethodPost, `{"email":"not an address","name":"news"}`)
	if err := bff.PostSubscription(bff.Config{})(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPostSubscriptionRejectsAFeedOutsideTheSite(t *testing.T) {
	ctx, rec := subscribeContext(t, http.MethodPost, `{"email":"reader@example.com","feed":"../secret.xml"}`)
	if err := bff.PostSubscription(bff.Config{})(ctx); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
