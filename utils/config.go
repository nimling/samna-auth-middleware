package utils

import (
	"net/url"
	"os"
	"sync"
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

	Issuer       string
	TokenURL     url.URL
	JWKSURL      url.URL
	AuthDisabled bool
}

var (
	cfg     Config
	cfgOnce sync.Once
)

func loadConfig() {
	cfg.Endpoint = os.Getenv("SAUTH_ENDPOINT")
	cfg.AppSlug = os.Getenv("SAUTH_APP_SLUG")
	cfg.AppSecret = os.Getenv("SAUTH_APP_SECRET")
	cfg.RedirectURI = os.Getenv("SAUTH_REDIRECT_URI")
	cfg.PageRedirect = os.Getenv("SAUTH_PAGE_REDIRECT")
	cfg.ClientTimeout = os.Getenv("SAUTH_CLIENT_TIMEOUT")
	cfg.DebugUserID = os.Getenv("SAUTH_DEBUG_USER_ID")
	cfg.DebugClients = os.Getenv("SAUTH_DEBUG_CLIENTS")
}

func Cfg() *Config {
	cfgOnce.Do(loadConfig)
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
