package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/revenuecat/cli/internal/config"
)

// setEnv sets envs and registers cleanup. Cleaner than per-test t.Setenv calls
// when many vars are touched at once.
func setEnv(t *testing.T, pairs map[string]string) {
	t.Helper()
	for k, v := range pairs {
		t.Setenv(k, v)
	}
}

func TestProfileName(t *testing.T) {
	t.Run("default when nothing set", func(t *testing.T) {
		t.Setenv("RC_PROFILE", "")
		if got := config.ProfileName(""); got != "default" {
			t.Errorf("want default, got %q", got)
		}
	})
	t.Run("env var honored when arg empty", func(t *testing.T) {
		t.Setenv("RC_PROFILE", "staging")
		if got := config.ProfileName(""); got != "staging" {
			t.Errorf("want staging, got %q", got)
		}
	})
	t.Run("explicit arg beats env var", func(t *testing.T) {
		t.Setenv("RC_PROFILE", "staging")
		if got := config.ProfileName("prod"); got != "prod" {
			t.Errorf("want prod, got %q", got)
		}
	})
}

func TestLoad_MissingFileReturnsZeroConfig(t *testing.T) {
	dir := t.TempDir()
	setEnv(t, map[string]string{
		"RC_CONFIG_DIR": dir,
		"RC_API_KEY":    "",
		"RC_PROJECT_ID": "",
		"RC_BASE_URL":   "",
	})
	cfg, err := config.Load("never-saved")
	if err != nil {
		t.Fatalf("Load on missing file should not error: %v", err)
	}
	if cfg.APIKey != "" || cfg.ProjectID != "" {
		t.Errorf("expected zero config, got %+v", cfg)
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	setEnv(t, map[string]string{
		"RC_CONFIG_DIR": dir,
		"RC_API_KEY":    "",
		"RC_PROJECT_ID": "",
		"RC_BASE_URL":   "",
	})
	want := &config.Config{APIKey: "sk_test_xyz", ProjectID: "proj_x", BaseURL: "https://example.test"}
	if err := config.Save("default", want); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	if *got != *want {
		t.Errorf("round-trip mismatch: want %+v, got %+v", want, got)
	}
}

func TestSave_WritesWithOwnerOnlyPermissions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", dir)
	if err := config.Save("default", &config.Config{APIKey: "secret"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "default.json"))
	if err != nil {
		t.Fatal(err)
	}
	// 0o600 is the contract — the file contains an API key.
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("want mode 0600, got %o", mode)
	}
}

func TestLoad_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", dir)
	t.Setenv("RC_API_KEY", "")
	t.Setenv("RC_PROJECT_ID", "")

	if err := config.Save("default", &config.Config{APIKey: "file_key", ProjectID: "file_proj"}); err != nil {
		t.Fatal(err)
	}

	// Now layer env on top.
	t.Setenv("RC_API_KEY", "env_key")
	t.Setenv("RC_PROJECT_ID", "env_proj")
	t.Setenv("RC_BASE_URL", "https://env.example")

	cfg, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "env_key" {
		t.Errorf("env should beat file: got %q", cfg.APIKey)
	}
	if cfg.ProjectID != "env_proj" {
		t.Errorf("env should beat file: got %q", cfg.ProjectID)
	}
	if cfg.BaseURL != "https://env.example" {
		t.Errorf("env BaseURL not applied: got %q", cfg.BaseURL)
	}
}

func TestLoad_CorruptFileErrors(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", dir)
	bad := filepath.Join(dir, "default.json")
	if err := os.WriteFile(bad, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load("default"); err == nil {
		t.Fatal("want error on corrupt JSON, got nil")
	}
}
