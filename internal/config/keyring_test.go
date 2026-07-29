package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/revenuecat/cli/internal/config"
)

// Use an in-memory keyring for the whole package so Save/Load never touch the
// real OS keychain during tests (no prompts, no pollution, deterministic in CI).
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}

// The security property: with a working keyring, secrets round-trip through
// Save/Load but never land in the plaintext profile file.
func TestSave_KeepsSecretsOutOfFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", dir)
	t.Setenv("RC_API_KEY", "")

	in := &config.Config{APIKey: "sk_secret", AccessToken: "atk_secret", RefreshToken: "rtk_secret", ProjectID: "proj_x"}
	if err := config.Save("default", in); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "default.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"sk_secret", "atk_secret", "rtk_secret"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("secret %q must not be written to the plaintext profile file", secret)
		}
	}
	if !strings.Contains(string(raw), "proj_x") {
		t.Error("non-secret fields should still be in the file")
	}

	out, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	if out.APIKey != "sk_secret" || out.AccessToken != "atk_secret" || out.RefreshToken != "rtk_secret" {
		t.Errorf("secrets should round-trip via the keyring: %+v", out)
	}
}
