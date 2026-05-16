package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	APIKey    string `json:"api_key,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
}

const defaultProfile = "default"

// ProfileName resolves the active profile name. Precedence (highest first):
//  1. explicit argument (e.g. --profile flag)
//  2. RC_PROFILE env var
//  3. .active pointer file in the config dir (written by `rc profiles use`)
//  4. "default"
func ProfileName(p string) string {
	if p != "" {
		return p
	}
	if env := os.Getenv("RC_PROFILE"); env != "" {
		return env
	}
	if name := readActivePointer(); name != "" {
		return name
	}
	return defaultProfile
}

func readActivePointer() string {
	dir, err := configDir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, ".active"))
	if err != nil {
		return ""
	}
	return string(bytesTrimSpace(b))
}

// bytesTrimSpace is a tiny stdlib-free trim — the `.active` file is at most
// a dozen bytes; reaching for strings.TrimSpace + a conversion is overkill.
func bytesTrimSpace(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\n' || b[0] == '\r' || b[0] == '\t') {
		b = b[1:]
	}
	for len(b) > 0 {
		c := b[len(b)-1]
		if c != ' ' && c != '\n' && c != '\r' && c != '\t' {
			break
		}
		b = b[:len(b)-1]
	}
	return b
}

// SetActiveProfile writes the .active pointer file. Errors if the named
// profile doesn't exist on disk (avoids "active" pointing at nothing).
func SetActiveProfile(name string) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, name+".json")); err != nil {
		return fmt.Errorf("profile %q does not exist on disk; create it with `rc login --profile %s`", name, name)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ".active"), []byte(name+"\n"), 0o600)
}

// ListProfiles returns the names of every profile file in the config dir.
func ListProfiles() ([]string, error) {
	dir, err := configDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !filepathIsJSON(n) {
			continue
		}
		names = append(names, n[:len(n)-5])
	}
	return names, nil
}

func filepathIsJSON(name string) bool {
	return len(name) > 5 && name[len(name)-5:] == ".json"
}

// DeleteProfile removes a profile file. If it was the active one, the
// .active pointer is cleared.
func DeleteProfile(name string) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, name+".json")); err != nil {
		return err
	}
	if readActivePointer() == name {
		_ = os.Remove(filepath.Join(dir, ".active"))
	}
	return nil
}

func configDir() (string, error) {
	if v := os.Getenv("RC_CONFIG_DIR"); v != "" {
		return v, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "revenuecat"), nil
}

func profilePath(profile string) (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ProfileName(profile)+".json"), nil
}

// Load reads a profile from disk, layering env vars on top.
// Missing files are not an error — they return a zero Config so first-run
// commands like `rc login` work cleanly.
func Load(profile string) (*Config, error) {
	cfg := &Config{BaseURL: ""}
	path, err := profilePath(profile)
	if err != nil {
		return nil, err
	}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, cfg); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if v := os.Getenv("RC_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv("RC_PROJECT_ID"); v != "" {
		cfg.ProjectID = v
	}
	if v := os.Getenv("RC_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	return cfg, nil
}

func Save(profile string, cfg *Config) error {
	path, err := profilePath(profile)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
