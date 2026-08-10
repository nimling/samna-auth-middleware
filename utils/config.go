package utils

import (
	"net/url"
	"os"
)

type Config struct {
	Endpoint      string
	AppSlug       string
	AppSecret     string
	RedirectURI   string
	PageRedirect  string
	ClientTimeout string
	DebugUserID   string
	DebugClients  string

	ConsentPolicyVersion string
	ConsentPolicyPath    string

	Issuer       string
	TokenURL     url.URL
	JWKSURL      url.URL
	AuthDisabled bool
}

var cfg Config

func loadConfig() {
	cfg.Endpoint = os.Getenv("SAUTH_ENDPOINT")
	cfg.AppSlug = os.Getenv("SAUTH_APP_SLUG")
	cfg.AppSecret = os.Getenv("SAUTH_APP_SECRET")
	cfg.RedirectURI = os.Getenv("SAUTH_REDIRECT_URI")
	cfg.PageRedirect = os.Getenv("SAUTH_PAGE_REDIRECT")
	cfg.ClientTimeout = os.Getenv("SAUTH_CLIENT_TIMEOUT")
	cfg.DebugUserID = os.Getenv("SAUTH_DEBUG_USER_ID")
	cfg.DebugClients = os.Getenv("SAUTH_DEBUG_CLIENTS")
	cfg.ConsentPolicyVersion = envOr("SAUTH_CONSENT_POLICY_VERSION", "1")
	cfg.ConsentPolicyPath = envOr("SAUTH_CONSENT_POLICY_PATH", "/legal/cookie-policy")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func Cfg() *Config {
	loadConfig()
	return &cfg
}

func SetOIDC(issuer string, tokenURL, jwksURL url.URL) {
	c := Cfg()
	c.Issuer = issuer
	c.TokenURL = tokenURL
	c.JWKSURL = jwksURL
	c.AuthDisabled = false
}

func SetAuthDisabled() {
	Cfg().AuthDisabled = true
}
