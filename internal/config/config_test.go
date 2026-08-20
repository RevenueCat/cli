package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/revenuecat/cli/internal/config"
)

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

	for _, name := range []string{"default", "staging"} {
		if err := config.Save(name, &config.Config{APIKey: "k_" + name}); err != nil {
			t.Fatal(err)
		}
	}

	if got := config.ProfileName(""); got != "default" {
		t.Errorf("with no pointer file, want 'default', got %q", got)
	}

	if err := config.SetActiveProfile("staging"); err != nil {
		t.Fatal(err)
	}
	if got := config.ProfileName(""); got != "staging" {
		t.Errorf("after SetActiveProfile(staging), want 'staging', got %q", got)
	}

	t.Setenv("RC_PROFILE", "default")
	if got := config.ProfileName(""); got != "default" {
		t.Errorf("env should beat pointer, got %q", got)
	}
	t.Setenv("RC_PROFILE", "")

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

func TestSave_DoesNotPersistEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	setEnv(t, map[string]string{
		"RC_CONFIG_DIR": dir, "RC_API_KEY": "", "RC_PROJECT_ID": "", "RC_BASE_URL": "", "RC_PROFILE": "",
	})

	if err := config.Save("default", &config.Config{APIKey: "sk_disk", ProjectID: "proj_disk"}); err != nil {
		t.Fatal(err)
	}

	setEnv(t, map[string]string{"RC_API_KEY": "sk_env", "RC_PROJECT_ID": "proj_env"})
	cfg, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	cfg.ProjectID = "proj_chosen"
	if err := config.Save("default", cfg); err != nil {
		t.Fatal(err)
	}

	setEnv(t, map[string]string{"RC_API_KEY": "", "RC_PROJECT_ID": ""})
	got, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	if got.APIKey != "sk_disk" {
		t.Errorf("env API key leaked to disk: got %q, want sk_disk", got.APIKey)
	}
	if got.ProjectID != "proj_chosen" {
		t.Errorf("explicit project change not persisted: got %q, want proj_chosen", got.ProjectID)
	}
}

func saveOAuthProfile(t *testing.T, name string) {
	t.Helper()
	if err := config.Save(name, &config.Config{
		TokenType:    "oauth",
		AccessToken:  "oauth_access_token",
		RefreshToken: "oauth_refresh_token",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCredential_OAuthBeatsEnvAPIKey(t *testing.T) {
	dir := t.TempDir()
	setEnv(t, map[string]string{"RC_CONFIG_DIR": dir, "RC_API_KEY": "", "RC_PROJECT_ID": "", "RC_BASE_URL": "", "RC_PROFILE": ""})
	saveOAuthProfile(t, "default")

	t.Setenv("RC_API_KEY", "sk_under_scoped_env")
	cfg, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	tok, src := cfg.Credential()
	if src != config.SourceOAuth {
		t.Errorf("want source oauth, got %q", src)
	}
	if tok != "oauth_access_token" {
		t.Errorf("want the OAuth token, got %q", tok)
	}
	if cfg.BearerToken() != "oauth_access_token" {
		t.Errorf("BearerToken should be the OAuth token, got %q", cfg.BearerToken())
	}
	present := cfg.PresentCredentialSources()
	if len(present) != 2 || present[0] != config.SourceOAuth || present[1] != config.SourceEnv {
		t.Errorf("want [oauth env] present, got %v", present)
	}
}

func TestCredential_EnvUsedWhenNoLogin(t *testing.T) {
	dir := t.TempDir()
	setEnv(t, map[string]string{"RC_CONFIG_DIR": dir, "RC_API_KEY": "", "RC_PROJECT_ID": "", "RC_BASE_URL": "", "RC_PROFILE": ""})

	t.Setenv("RC_API_KEY", "sk_ci_env")
	cfg, err := config.Load("never-saved")
	if err != nil {
		t.Fatal(err)
	}
	tok, src := cfg.Credential()
	if src != config.SourceEnv || tok != "sk_ci_env" {
		t.Errorf("CI path: want env/sk_ci_env, got %q/%q", src, tok)
	}
	if got := cfg.PresentCredentialSources(); len(got) != 1 || got[0] != config.SourceEnv {
		t.Errorf("want only [env], got %v", got)
	}
}

func TestCredential_StoredProfileKey(t *testing.T) {
	dir := t.TempDir()
	setEnv(t, map[string]string{"RC_CONFIG_DIR": dir, "RC_API_KEY": "", "RC_PROJECT_ID": "", "RC_BASE_URL": "", "RC_PROFILE": ""})
	if err := config.Save("default", &config.Config{APIKey: "sk_disk"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	if tok, src := cfg.Credential(); src != config.SourceProfile || tok != "sk_disk" {
		t.Errorf("want profile/sk_disk, got %q/%q", src, tok)
	}
}

func TestCredential_FlagBeatsEverything(t *testing.T) {
	dir := t.TempDir()
	setEnv(t, map[string]string{"RC_CONFIG_DIR": dir, "RC_API_KEY": "", "RC_PROJECT_ID": "", "RC_BASE_URL": "", "RC_PROFILE": ""})
	saveOAuthProfile(t, "default")

	t.Setenv("RC_API_KEY", "sk_env")
	cfg, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	cfg.SetFlagAPIKey("sk_flag")
	tok, src := cfg.Credential()
	if src != config.SourceFlag || tok != "sk_flag" {
		t.Errorf("flag should win, got %q/%q", src, tok)
	}
	present := cfg.PresentCredentialSources()
	if len(present) != 3 {
		t.Errorf("want flag+oauth+env present, got %v", present)
	}
}

func TestSetAPIKey_SupersedesEnvOverride(t *testing.T) {
	dir := t.TempDir()
	setEnv(t, map[string]string{"RC_CONFIG_DIR": dir, "RC_API_KEY": "", "RC_PROJECT_ID": "", "RC_BASE_URL": "", "RC_PROFILE": ""})

	t.Setenv("RC_API_KEY", "sk_env")
	cfg, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	cfg.SetAPIKey("sk_typed")
	if tok, src := cfg.Credential(); src != config.SourceProfile || tok != "sk_typed" {
		t.Errorf("want profile/sk_typed after SetAPIKey, got %q/%q", src, tok)
	}
	if err := config.Save("default", cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RC_API_KEY", "")
	got, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	if got.APIKey != "sk_typed" {
		t.Errorf("SetAPIKey value should persist to disk, got %q", got.APIKey)
	}
}

func writeProjectFile(t *testing.T, dir, projectID string) {
	t.Helper()
	body := []byte(`{"project_id": "` + projectID + `"}`)
	if err := os.WriteFile(filepath.Join(dir, config.ProjectFileName), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func clearProjectEnv(t *testing.T) {
	t.Helper()
	setEnv(t, map[string]string{"RC_API_KEY": "", "RC_PROJECT_ID": "", "RC_BASE_URL": "", "RC_PROFILE": ""})
}

func TestLoad_PerDirProjectFile(t *testing.T) {
	t.Run("found in a parent directory", func(t *testing.T) {
		t.Setenv("RC_CONFIG_DIR", t.TempDir())
		clearProjectEnv(t)

		root := t.TempDir()
		writeProjectFile(t, root, "proj_dir")
		nested := filepath.Join(root, "a", "b", "c")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(nested)

		cfg, err := config.Load("default")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ProjectID != "proj_dir" {
			t.Errorf("per-dir file in parent should be found by walking up: got %q", cfg.ProjectID)
		}
	})

	t.Run("nearest file wins", func(t *testing.T) {
		t.Setenv("RC_CONFIG_DIR", t.TempDir())
		clearProjectEnv(t)

		root := t.TempDir()
		writeProjectFile(t, root, "proj_far")
		nested := filepath.Join(root, "child")
		if err := os.MkdirAll(nested, 0o755); err != nil {
			t.Fatal(err)
		}
		writeProjectFile(t, nested, "proj_near")
		t.Chdir(nested)

		cfg, err := config.Load("default")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ProjectID != "proj_near" {
			t.Errorf("nearest .revenuecat.json should win: got %q, want proj_near", cfg.ProjectID)
		}
	})

	t.Run("beats profile default", func(t *testing.T) {
		t.Setenv("RC_CONFIG_DIR", t.TempDir())
		clearProjectEnv(t)
		if err := config.Save("default", &config.Config{ProjectID: "proj_profile"}); err != nil {
			t.Fatal(err)
		}

		dir := t.TempDir()
		writeProjectFile(t, dir, "proj_dir")
		t.Chdir(dir)

		cfg, err := config.Load("default")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ProjectID != "proj_dir" {
			t.Errorf("per-dir file should beat profile default: got %q, want proj_dir", cfg.ProjectID)
		}
	})

	t.Run("env beats per-dir file", func(t *testing.T) {
		t.Setenv("RC_CONFIG_DIR", t.TempDir())
		clearProjectEnv(t)

		dir := t.TempDir()
		writeProjectFile(t, dir, "proj_dir")
		t.Chdir(dir)
		t.Setenv("RC_PROJECT_ID", "proj_env")

		cfg, err := config.Load("default")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ProjectID != "proj_env" {
			t.Errorf("RC_PROJECT_ID should beat per-dir file: got %q, want proj_env", cfg.ProjectID)
		}
	})

	t.Run("absent file falls back to profile", func(t *testing.T) {
		t.Setenv("RC_CONFIG_DIR", t.TempDir())
		clearProjectEnv(t)
		if err := config.Save("default", &config.Config{ProjectID: "proj_profile"}); err != nil {
			t.Fatal(err)
		}

		t.Chdir(t.TempDir())

		cfg, err := config.Load("default")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ProjectID != "proj_profile" {
			t.Errorf("with no per-dir file, should fall back to profile default: got %q", cfg.ProjectID)
		}
	})
}

func TestLoad_MalformedPerDirFileErrors(t *testing.T) {
	t.Run("invalid JSON", func(t *testing.T) {
		t.Setenv("RC_CONFIG_DIR", t.TempDir())
		clearProjectEnv(t)
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, config.ProjectFileName), []byte("not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)
		cfg, err := config.Load("default")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ProjectBindingError(false) == nil {
			t.Fatal("want error on malformed .revenuecat.json, got nil")
		}
	})

	t.Run("missing project_id key", func(t *testing.T) {
		t.Setenv("RC_CONFIG_DIR", t.TempDir())
		clearProjectEnv(t)
		if err := config.Save("default", &config.Config{ProjectID: "proj_profile"}); err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, config.ProjectFileName), []byte(`{"projectId": "typo_key"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)
		cfg, err := config.Load("default")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ProjectBindingError(false) == nil {
			t.Fatal("want error when .revenuecat.json has no project_id, got nil")
		}
	})
}

func TestProjectBindingError_HigherOverridesBypassMalformedFile(t *testing.T) {
	writeBad := func(t *testing.T) {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, config.ProjectFileName), []byte("not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)
	}

	t.Run("RC_PROJECT_ID wins over malformed file", func(t *testing.T) {
		t.Setenv("RC_CONFIG_DIR", t.TempDir())
		clearProjectEnv(t)
		writeBad(t)
		t.Setenv("RC_PROJECT_ID", "proj_env")

		cfg, err := config.Load("default")
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ProjectID != "proj_env" {
			t.Errorf("RC_PROJECT_ID should apply despite malformed file, got %q", cfg.ProjectID)
		}
		if err := cfg.ProjectBindingError(false); err != nil {
			t.Errorf("malformed file must not block RC_PROJECT_ID, got %v", err)
		}
	})

	t.Run("--project-id flag bypasses malformed file", func(t *testing.T) {
		t.Setenv("RC_CONFIG_DIR", t.TempDir())
		clearProjectEnv(t)
		writeBad(t)

		cfg, err := config.Load("default")
		if err != nil {
			t.Fatal(err)
		}
		if err := cfg.ProjectBindingError(true); err != nil {
			t.Errorf("malformed file must not block an explicit --project-id, got %v", err)
		}
	})
}

func TestLogin_FlagProjectMatchingDirBindingPersists(t *testing.T) {
	setEnv(t, map[string]string{"RC_CONFIG_DIR": t.TempDir(), "RC_API_KEY": "", "RC_PROJECT_ID": "", "RC_BASE_URL": "", "RC_PROFILE": ""})
	if err := config.Save("default", &config.Config{ProjectID: "proj_old"}); err != nil {
		t.Fatal(err)
	}

	work := t.TempDir()
	writeProjectFile(t, work, "proj_dir")
	t.Chdir(work)

	cfg, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	// Mirrors login (clearProjectBinding) adopting --project-id equal to the tree binding.
	cfg.UseProjectID("proj_dir")
	if err := config.Save("default", cfg); err != nil {
		t.Fatal(err)
	}

	t.Chdir(t.TempDir())
	got, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != "proj_dir" {
		t.Errorf("--project-id matching the dir binding must persist over the old default, got %q", got.ProjectID)
	}
}

func TestStoredProjectID_UnwindsOverlay(t *testing.T) {
	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	clearProjectEnv(t)
	if err := config.Save("default", &config.Config{ProjectID: "proj_profile"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeProjectFile(t, dir, "proj_dir")
	t.Chdir(dir)
	t.Setenv("RC_PROJECT_ID", "proj_env")

	cfg, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProjectID != "proj_env" {
		t.Fatalf("setup: want env overlay active, got %q", cfg.ProjectID)
	}
	if got := cfg.StoredProjectID(); got != "proj_profile" {
		t.Errorf("StoredProjectID should unwind overlays to the profile default, got %q", got)
	}
}

func TestLoadStored_IgnoresPerDirBindingAndEnv(t *testing.T) {
	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	clearProjectEnv(t)
	if err := config.Save("default", &config.Config{ProjectID: "proj_profile"}); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	writeProjectFile(t, dir, "proj_dir")
	t.Chdir(dir)
	t.Setenv("RC_PROJECT_ID", "proj_env")

	cfg, err := config.LoadStored("default")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProjectID != "proj_profile" {
		t.Errorf("LoadStored should report the profile's stored default, got %q", cfg.ProjectID)
	}
}

func TestSave_DoesNotPersistPerDirProject(t *testing.T) {
	setEnv(t, map[string]string{"RC_CONFIG_DIR": t.TempDir(), "RC_API_KEY": "", "RC_PROJECT_ID": "", "RC_BASE_URL": "", "RC_PROFILE": ""})
	if err := config.Save("default", &config.Config{APIKey: "sk_disk", ProjectID: "proj_profile"}); err != nil {
		t.Fatal(err)
	}

	work := t.TempDir()
	writeProjectFile(t, work, "proj_dir")
	t.Chdir(work)

	cfg, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProjectID != "proj_dir" {
		t.Fatalf("setup: want per-dir project active, got %q", cfg.ProjectID)
	}
	if err := config.Save("default", cfg); err != nil {
		t.Fatal(err)
	}

	cfg2, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	cfg2.ProjectID = "proj_chosen"
	if err := config.Save("default", cfg2); err != nil {
		t.Fatal(err)
	}

	t.Chdir(t.TempDir())
	got, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != "proj_chosen" {
		t.Errorf("want explicit change persisted and per-dir project reverted, got %q", got.ProjectID)
	}
}

func TestUseProjectID_PersistsChoiceEqualToDirBinding(t *testing.T) {
	setEnv(t, map[string]string{"RC_CONFIG_DIR": t.TempDir(), "RC_API_KEY": "", "RC_PROJECT_ID": "", "RC_BASE_URL": "", "RC_PROFILE": ""})
	if err := config.Save("default", &config.Config{APIKey: "sk_disk", ProjectID: "proj_profile"}); err != nil {
		t.Fatal(err)
	}

	work := t.TempDir()
	writeProjectFile(t, work, "proj_dir")
	t.Chdir(work)

	cfg, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	// User explicitly selects the project the directory already binds to.
	cfg.UseProjectID("proj_dir")
	if err := config.Save("default", cfg); err != nil {
		t.Fatal(err)
	}

	t.Chdir(t.TempDir())
	got, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != "proj_dir" {
		t.Errorf("explicit UseProjectID matching the dir binding must persist, got %q", got.ProjectID)
	}
}

func TestOverrideProjectID_NotPersistedByIncidentalSave(t *testing.T) {
	setEnv(t, map[string]string{"RC_CONFIG_DIR": t.TempDir(), "RC_API_KEY": "", "RC_PROJECT_ID": "", "RC_BASE_URL": "", "RC_PROFILE": ""})
	if err := config.Save("default", &config.Config{ProjectID: "proj_profile"}); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	// A general command runs with --project-id; the override is in effect...
	cfg.OverrideProjectID("proj_flag")
	if cfg.ProjectID != "proj_flag" {
		t.Fatalf("override should take effect for this invocation, got %q", cfg.ProjectID)
	}
	if got := cfg.StoredProjectID(); got != "proj_profile" {
		t.Errorf("override must not change the stored default, StoredProjectID got %q", got)
	}
	// ...and then something persists config (e.g. an OAuth token refresh).
	if err := config.Save("default", cfg); err != nil {
		t.Fatal(err)
	}

	got, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != "proj_profile" {
		t.Errorf("a one-shot --project-id must not be baked into the profile default, got %q", got.ProjectID)
	}
}

func TestOverrideProjectID_MatchingDirBindingNotPersisted(t *testing.T) {
	setEnv(t, map[string]string{"RC_CONFIG_DIR": t.TempDir(), "RC_API_KEY": "", "RC_PROJECT_ID": "", "RC_BASE_URL": "", "RC_PROFILE": ""})
	if err := config.Save("default", &config.Config{ProjectID: "proj_profile"}); err != nil {
		t.Fatal(err)
	}

	work := t.TempDir()
	writeProjectFile(t, work, "proj_dir")
	t.Chdir(work)

	cfg, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	// --project-id equal to the tree binding, on a non-login command.
	cfg.OverrideProjectID("proj_dir")
	if err := config.Save("default", cfg); err != nil {
		t.Fatal(err)
	}

	t.Chdir(t.TempDir())
	got, err := config.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != "proj_profile" {
		t.Errorf("--project-id matching the dir binding must revert on an incidental save, got %q", got.ProjectID)
	}
}

func TestProfileName_RejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	setEnv(t, map[string]string{"RC_CONFIG_DIR": dir, "RC_PROFILE": "", "RC_API_KEY": ""})
	for _, bad := range []string{"../evil", "a/b", "..", `a\b`} {
		if _, err := config.Load(bad); err == nil {
			t.Errorf("Load(%q) should reject an unsafe profile name", bad)
		}
		if err := config.Save(bad, &config.Config{}); err == nil {
			t.Errorf("Save(%q) should reject an unsafe profile name", bad)
		}
	}
	if err := config.Save("staging", &config.Config{ProjectID: "p"}); err != nil {
		t.Errorf("a normal profile name must still work: %v", err)
	}
}
