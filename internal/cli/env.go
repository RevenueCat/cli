package cli

import (
	"os"

	"github.com/revenuecat/cli/internal/buildinfo"
)

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// devEnvOrDefault honors an endpoint override env var only in dev builds; a
// shipped binary always uses the production default.
func devEnvOrDefault(name, fallback string) string {
	if !buildinfo.IsDev() {
		return fallback
	}
	return envOrDefault(name, fallback)
}
