package echo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	sclient "github.com/nimling/samna-auth-middleware/client"
)

func TestPostClientCredentialsSendsFormCredentialsWithoutAppHeaders(t *testing.T) {
	var grantType, clientID, clientSecret, appSlug string

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		grantType = r.Form.Get("grant_type")
		clientID = r.Form.Get("client_id")
		clientSecret = r.Form.Get("client_secret")
		appSlug = r.Header.Get("SAuth-App-Slug")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":{"access_token":"tok","token_type":"Bearer","access_expires_in":3600},"count":1,"request_id":"00000000-0000-0000-0000-000000000000"}`))
	}))
	defer tokenServer.Close()

	token, err := sclient.PostClientCredentials(context.Background(), tokenServer.URL, "the-client", "the-secret")
	if err != nil {
		t.Fatalf("PostClientCredentials returned %v", err)
	}
	if token.AccessToken != "tok" {
		t.Errorf("access token = %q, want tok", token.AccessToken)
	}
	if grantType != "client_credentials" {
		t.Errorf("grant_type = %q, want client_credentials", grantType)
	}
	if clientID != "the-client" || clientSecret != "the-secret" {
		t.Errorf("credentials = %q/%q, want the-client/the-secret", clientID, clientSecret)
	}
	if appSlug != "" {
		t.Errorf("SAuth-App-Slug = %q, want it absent for a client grant", appSlug)
	}
}

func TestPostClientCredentialsReportsUnauthorized(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer tokenServer.Close()

	if _, err := sclient.PostClientCredentials(context.Background(), tokenServer.URL, "id", "secret"); err == nil {
		t.Fatal("PostClientCredentials returned no error on 401")
	}
}
