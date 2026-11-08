package utils

import (
	"net/url"
	"os"
)

type Config struct {
	Endpoint      string
	AppSlug       string
	AppSecret     string
	ClientID      string
	ClientSecret  string
	RedirectURI   string
	PageRedirect  string
	ClientTimeout string
	DebugUserID   string
	DebugClients  string
	DebugSeed     string
	ReplicaDSN    string

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
	cfg.ClientID = os.Getenv("SAUTH_CLIENT_ID")
	cfg.ClientSecret = os.Getenv("SAUTH_CLIENT_SECRET")
	cfg.RedirectURI = os.Getenv("SAUTH_REDIRECT_URI")
	cfg.PageRedirect = os.Getenv("SAUTH_PAGE_REDIRECT")
	cfg.ClientTimeout = os.Getenv("SAUTH_CLIENT_TIMEOUT")
	cfg.DebugUserID = os.Getenv("SAUTH_DEBUG_USER_ID")
	cfg.DebugClients = os.Getenv("SAUTH_DEBUG_CLIENTS")
	cfg.DebugSeed = os.Getenv("SAUTH_DEBUG_PRIVATE_SEED")
	cfg.ReplicaDSN = os.Getenv("SAUTH_REPLICA_DSN")
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
