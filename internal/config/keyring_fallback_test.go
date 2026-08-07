package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/revenuecat/cli/internal/config"
)

// When the OS keyring is unavailable (headless Linux, no D-Bus, Docker, a
// locked store), Save must keep the secrets in the 0600 profile file and Load
// must read them back — the documented keychain-with-fallback model.
func TestSave_FallsBackToFileWhenKeyringUnavailable(t *testing.T) {
	keyring.MockInitWithError(errors.New("keyring unavailable"))
	t.Cleanup(keyring.MockInit) // restore the in-memory keyring for other tests

	dir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", dir)
	t.Setenv("RC_API_KEY", "")

	in := &config.Config{APIKey: "sk_secret", AccessToken: "atk_secret", ProjectID: "proj_x"}
	if err := config.Save("default", in); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "default.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "sk_secret") || !strings.Contains(string(raw), "atk_secret") {
		t.Fatalf("with no keyring, secrets must fall back into the file:\n%s", raw)
	}

	out, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	if out.APIKey != "sk_secret" || out.AccessToken != "atk_secret" {
		t.Errorf("secrets should load from the file fallback: %+v", out)
	}
}
