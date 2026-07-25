package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// validateProfileName rejects names that would escape the config dir when
// joined into a file path (path separators, "..", empty). Profile names come
// from --profile, RC_PROFILE, or the .active file, so an unchecked name like
// "../../x" would read/write outside ~/.config/revenuecat.
func validateProfileName(name string) error {
	if name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return fmt.Errorf("invalid profile name %q: must not be empty or contain path separators", name)
	}
	return nil
}

type Config struct {
	// API key auth (original).
	APIKey string `json:"api_key,omitempty"`

	// OAuth auth. All fields are omitempty so old profiles remain valid.
	AccessToken    string    `json:"access_token,omitempty"`
	RefreshToken   string    `json:"refresh_token,omitempty"`
	TokenExpiresAt time.Time `json:"token_expires_at,omitempty"`
	TokenType      string    `json:"token_type,omitempty"` // "oauth" | "" (api_key)
	AccountEmail   string    `json:"account_email,omitempty"`
	AccountName    string    `json:"account_name,omitempty"`

	ProjectID string `json:"project_id,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`

	// Provenance of env-var overrides applied at Load. Unexported (never
	// serialized) and comparable (Config stays usable with ==). RC_API_KEY /
	// RC_PROJECT_ID / RC_BASE_URL override for a single invocation; without
	// this, any command that calls Save would bake the ephemeral env value
	// permanently into the profile. See Save.
	envAPIKey    envOverride
	envProjectID envOverride
	envBaseURL   envOverride
}

// envOverride records that Load replaced a field with an env-var value,
// keeping the pre-override on-disk value so Save can restore it rather than
// persist the ephemeral env value.
type envOverride struct {
	env, disk string
	set       bool
}

// BearerToken returns whichever auth credential should be sent as the
// Authorization: Bearer header. OAuth access token takes priority.
func (c *Config) BearerToken() string {
	if c.TokenType == "oauth" && c.AccessToken != "" {
		return c.AccessToken
	}
	return c.APIKey
}

// IsOAuth reports whether this profile holds OAuth credentials.
func (c *Config) IsOAuth() bool {
	return c.TokenType == "oauth"
}

// NeedsRefresh reports whether the OAuth access token is expired or within
// 5 minutes of expiry and a refresh token is available.
func (c *Config) NeedsRefresh() bool {
	if !c.IsOAuth() || c.RefreshToken == "" {
		return false
	}
	if c.TokenExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(5 * time.Minute).After(c.TokenExpiresAt)
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
	if err := validateProfileName(name); err != nil {
		return err
	}
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
	if err := validateProfileName(name); err != nil {
		return err
	}
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

// configDir is ~/.config/revenuecat on every platform (honoring
// XDG_CONFIG_HOME), following developer-CLI convention rather than
// os.UserConfigDir's platform-specific locations, so docs and support can
// reference one path everywhere. RC_CONFIG_DIR overrides.
func configDir() (string, error) {
	if v := os.Getenv("RC_CONFIG_DIR"); v != "" {
		return v, nil
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "revenuecat"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "revenuecat"), nil
}

func profilePath(profile string) (string, error) {
	name := ProfileName(profile)
	if err := validateProfileName(name); err != nil {
		return "", err
	}
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".json"), nil
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
	// Credentials live in the OS keyring when available; overlay them onto
	// whatever the file held (the file carries them only as a fallback).
	loadSecrets(profile, cfg)
	applyEnv := func(o *envOverride, env string, cur *string) {
		if env == "" {
			return
		}
		*o = envOverride{env: env, disk: *cur, set: true}
		*cur = env
	}
	applyEnv(&cfg.envAPIKey, os.Getenv("RC_API_KEY"), &cfg.APIKey)
	applyEnv(&cfg.envProjectID, os.Getenv("RC_PROJECT_ID"), &cfg.ProjectID)
	applyEnv(&cfg.envBaseURL, os.Getenv("RC_BASE_URL"), &cfg.BaseURL)
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
	// Persist a copy with ephemeral env overrides reverted: a field still
	// holding its env-injected value means no command changed it this run, so
	// the on-disk value (not the env var) is what belongs in the profile. Do
	// this before storeSecrets so an env-injected key isn't written to the
	// keyring either.
	out := *cfg
	revert := func(o envOverride, cur *string) {
		if o.set && *cur == o.env {
			*cur = o.disk
		}
	}
	revert(cfg.envAPIKey, &out.APIKey)
	revert(cfg.envProjectID, &out.ProjectID)
	revert(cfg.envBaseURL, &out.BaseURL)

	// Try the OS keyring first; on success the file omits the secrets, on
	// failure (headless/Docker) they stay in the 0600 file as a fallback.
	toWrite := &out
	if storeSecrets(profile, &out) {
		redacted := out
		redacted.APIKey, redacted.AccessToken, redacted.RefreshToken = "", "", ""
		toWrite = &redacted
	}
	b, err := json.MarshalIndent(toWrite, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// statePath returns the path of a named auxiliary state file stored beside
// the profile files (e.g. the last Rico conversation per profile).
func statePath(profile, name string) (string, error) {
	pname := ProfileName(profile)
	if err := validateProfileName(pname); err != nil {
		return "", err
	}
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, pname+"."+name+".json"), nil
}

// LoadState reads a named state file into out. A missing file is not an
// error; out is left untouched.
func LoadState(profile, name string, out any) error {
	path, err := statePath(profile, name)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(b, out)
}

// SaveState writes a named state file beside the profile files.
func SaveState(profile, name string, v any) error {
	path, err := statePath(profile, name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
