package client_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	sclient "github.com/nimling/samna-auth-middleware/v2/client"
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
		w.Write([]byte(`{"access_token":"tok","token_type":"Bearer","expires_in":3600}`))
	}))
	defer tokenServer.Close()

	token, err := sclient.PostClientCredentials(context.Background(), tokenServer.URL, "the-client", "the-secret")
	if err != nil {
		t.Fatalf("PostClientCredentials returned %v", err)
	}
	if token.AccessToken != "tok" {
		t.Errorf("access token = %q, want tok", token.AccessToken)
	}
	if token.AccessExpiresIn != 3600 {
		t.Errorf("expires in = %d, want 3600 from the RFC 6749 expires_in", token.AccessExpiresIn)
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

func TestPostClientCredentialsReadsTheTokenErrorBody(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant","error_description":"expired"}`))
	}))
	defer tokenServer.Close()

	_, err := sclient.PostClientCredentials(context.Background(), tokenServer.URL, "id", "secret")
	var tokenErr *sclient.TokenError
	if !errors.As(err, &tokenErr) || tokenErr.Code != "invalid_grant" || tokenErr.Description != "expired" {
		t.Fatalf("error = %v, want the RFC 6749 invalid_grant body", err)
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
