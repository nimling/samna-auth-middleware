package bff

import (
	"testing"

	"github.com/nimling/samna-auth-middleware/bff"
)

func TestGuardedPathsDefaultToTheGuardedBundleSegment(t *testing.T) {
	t.Setenv("SAUTH_BFF_PORT", "")
	t.Setenv("SAUTH_BFF_BASE_PATH", "")
	t.Setenv("SAUTH_BFF_GUARDED_PATHS", "")

	config := bff.ConfigFromEnv()

	if len(config.GuardedPaths) != 1 || config.GuardedPaths[0] != "/_guarded" {
		t.Fatalf("guarded paths = %v, want [/_guarded]", config.GuardedPaths)
	}
	if config.Port != "4300" {
		t.Fatalf("port = %q, want 4300", config.Port)
	}
	if config.BasePath != "/api/sauth" {
		t.Fatalf("base path = %q, want /api/sauth", config.BasePath)
	}
}

func TestGuardedPathsReadACommaSeparatedList(t *testing.T) {
	t.Setenv("SAUTH_BFF_GUARDED_PATHS", "/_guarded, /internal ,")

	config := bff.ConfigFromEnv()

	if len(config.GuardedPaths) != 2 {
		t.Fatalf("guarded paths = %v, want two entries", config.GuardedPaths)
	}
	if config.GuardedPaths[0] != "/_guarded" || config.GuardedPaths[1] != "/internal" {
		t.Fatalf("guarded paths = %v, want [/_guarded /internal]", config.GuardedPaths)
	}
}

func TestPathGuardedMatchesOnPrefix(t *testing.T) {
	config := bff.Config{GuardedPaths: []string{"/_guarded", "/internal"}}

	guarded := []string{"/_guarded", "/_guarded/staff/page", "/internal/notes"}
	for _, path := range guarded {
		if !config.PathGuarded(path) {
			t.Fatalf("path %q should be guarded", path)
		}
	}

	public := []string{"/", "/guide/intro", "/guarded", "/api/sauth/login"}
	for _, path := range public {
		if config.PathGuarded(path) {
			t.Fatalf("path %q should be public", path)
		}
	}
}

func TestNoGuardedPathsLeavesEverythingPublic(t *testing.T) {
	config := bff.Config{}

	if config.PathGuarded("/_guarded/staff") {
		t.Fatal("an empty guarded list should leave every path public")
	}
}
