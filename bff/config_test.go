package bff_test

import (
	"testing"

	"github.com/nimling/samna-auth-middleware/bff"
)

func TestConfigDefaultsThePortAndBasePath(t *testing.T) {
	t.Setenv("SAUTH_BFF_PORT", "")
	t.Setenv("SAUTH_BFF_BASE_PATH", "")

	config := bff.ConfigFromEnv()

	if config.Port != "4300" {
		t.Fatalf("port = %q, want 4300", config.Port)
	}
	if config.BasePath != "/api/auth" {
		t.Fatalf("base path = %q, want /api/auth", config.BasePath)
	}
}

func TestConfigKeepsTheValuesTheEnvironmentSets(t *testing.T) {
	t.Setenv("SAUTH_BFF_PORT", "4400")
	t.Setenv("SAUTH_BFF_BASE_PATH", "/auth")
	t.Setenv("SAUTH_BFF_PROXY_TARGET", "http://localhost:5173")
	t.Setenv("SAUTH_BFF_STATIC_DIR", "./dist")

	config := bff.ConfigFromEnv()

	if config.Port != "4400" {
		t.Fatalf("port = %q, want 4400", config.Port)
	}
	if config.BasePath != "/auth" {
		t.Fatalf("base path = %q, want /auth", config.BasePath)
	}
	if config.ProxyTarget != "http://localhost:5173" {
		t.Fatalf("proxy target = %q, want the dev server", config.ProxyTarget)
	}
	if config.StaticDir != "./dist" {
		t.Fatalf("static dir = %q, want ./dist", config.StaticDir)
	}
}
