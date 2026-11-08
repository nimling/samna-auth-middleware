package client

import (
	"testing"
	"time"
)

func TestRequestTimeout_DefaultWhenUnset(t *testing.T) {
	t.Setenv("SAUTH_CLIENT_TIMEOUT", "")
	if got := requestTimeout(); got != 5*time.Second {
		t.Fatalf("default timeout: got %v want 5s", got)
	}
}

func TestRequestTimeout_EnvOverride(t *testing.T) {
	t.Setenv("SAUTH_CLIENT_TIMEOUT", "12")
	if got := requestTimeout(); got != 12*time.Second {
		t.Fatalf("env timeout: got %v want 12s", got)
	}
}

func TestRequestTimeout_InvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("SAUTH_CLIENT_TIMEOUT", "abc")
	if got := requestTimeout(); got != 5*time.Second {
		t.Fatalf("invalid timeout: got %v want 5s default", got)
	}
}

func TestRequestTimeout_NonPositiveFallsBackToDefault(t *testing.T) {
	t.Setenv("SAUTH_CLIENT_TIMEOUT", "0")
	if got := requestTimeout(); got != 5*time.Second {
		t.Fatalf("zero timeout: got %v want 5s default", got)
	}
}
