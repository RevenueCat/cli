package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	APIKey    string `json:"api_key,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
}

const defaultProfile = "default"

func ProfileName(p string) string {
	if p == "" {
		if env := os.Getenv("RC_PROFILE"); env != "" {
			return env
		}
		return defaultProfile
	}
	return p
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
