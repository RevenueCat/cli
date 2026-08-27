package cli

import (
	"testing"

	"github.com/revenuecat/cli/internal/buildinfo"
)

func TestDevEnvOrDefault(t *testing.T) {
	orig := buildinfo.Version
	defer func() { buildinfo.Version = orig }()
	t.Setenv("RC_TEST_ENDPOINT", "https://staging.example")

	buildinfo.Version = "dev"
	if got := devEnvOrDefault("RC_TEST_ENDPOINT", "https://prod.example"); got != "https://staging.example" {
		t.Errorf("dev build: got %q, want the override", got)
	}

	buildinfo.Version = "1.2.3"
	if got := devEnvOrDefault("RC_TEST_ENDPOINT", "https://prod.example"); got != "https://prod.example" {
		t.Errorf("release build: got %q, want the production default", got)
	}
}
