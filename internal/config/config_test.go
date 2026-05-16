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

func TestActiveProfilePointer_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", dir)
	t.Setenv("RC_PROFILE", "")

	// Create two profiles.
	for _, name := range []string{"default", "staging"} {
		if err := config.Save(name, &config.Config{APIKey: "k_" + name}); err != nil {
			t.Fatal(err)
		}
	}

	// No pointer yet → fall back to "default".
	if got := config.ProfileName(""); got != "default" {
		t.Errorf("with no pointer file, want 'default', got %q", got)
	}

	// Set staging as active.
	if err := config.SetActiveProfile("staging"); err != nil {
		t.Fatal(err)
	}
	if got := config.ProfileName(""); got != "staging" {
		t.Errorf("after SetActiveProfile(staging), want 'staging', got %q", got)
	}

	// Env var beats pointer.
	t.Setenv("RC_PROFILE", "default")
	if got := config.ProfileName(""); got != "default" {
		t.Errorf("env should beat pointer, got %q", got)
	}
	t.Setenv("RC_PROFILE", "")

	// Explicit arg beats both.
	if got := config.ProfileName("override"); got != "override" {
		t.Errorf("explicit arg should beat pointer, got %q", got)
	}
}

func TestSetActiveProfile_RejectsNonexistent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", dir)
	if err := config.SetActiveProfile("does-not-exist"); err == nil {
		t.Fatal("expected error for nonexistent profile")
	}
}

func TestListProfiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", dir)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if err := config.Save(name, &config.Config{}); err != nil {
			t.Fatal(err)
		}
	}
	names, err := config.ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 3 {
		t.Errorf("want 3 profiles, got %d: %v", len(names), names)
	}
}

func TestDeleteProfile_ClearsActivePointer(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", dir)
	t.Setenv("RC_PROFILE", "")
	if err := config.Save("doomed", &config.Config{APIKey: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := config.SetActiveProfile("doomed"); err != nil {
		t.Fatal(err)
	}
	if got := config.ProfileName(""); got != "doomed" {
		t.Fatalf("setup: want 'doomed' active, got %q", got)
	}
	if err := config.DeleteProfile("doomed"); err != nil {
		t.Fatal(err)
	}
	if got := config.ProfileName(""); got != "default" {
		t.Errorf("after deleting active profile, should fall back to 'default'; got %q", got)
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
