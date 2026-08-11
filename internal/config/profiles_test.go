package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListProfiles_SkipsStateFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", dir)
	for _, name := range []string{"default.json", "work.json", "default.rico.json", "work.paywalls.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"default": true, "work": true}
	if len(got) != len(want) {
		t.Fatalf("profiles = %v, want default+work only (state files must be skipped)", got)
	}
	for _, name := range got {
		if !want[name] {
			t.Fatalf("state file leaked into profiles as %q: %v", name, got)
		}
	}
}

func TestProfileName_IgnoresInvalidActiveAndEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", dir)
	t.Setenv("RC_PROFILE", "")
	// residual dotted name from the old phantom-profile bug
	if err := os.WriteFile(filepath.Join(dir, ".active"), []byte("default.rico\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ProfileName(""); got != "default" {
		t.Fatalf("ProfileName = %q, want default (invalid .active must be ignored)", got)
	}
	if _, err := Load(""); err != nil {
		t.Fatalf("Load errored on invalid .active instead of self-healing: %v", err)
	}

	t.Setenv("RC_PROFILE", "bad.name")
	if got := ProfileName(""); got != "default" {
		t.Fatalf("ProfileName = %q, want default (invalid RC_PROFILE must be ignored)", got)
	}
}

func TestValidateProfileName_RejectsDotted(t *testing.T) {
	if err := validateProfileName("default"); err != nil {
		t.Fatalf("plain name rejected: %v", err)
	}
	for _, bad := range []string{"default.rico", "a.b", ".", "..", "", "a/b"} {
		if err := validateProfileName(bad); err == nil {
			t.Fatalf("expected %q to be rejected", bad)
		}
	}
}
