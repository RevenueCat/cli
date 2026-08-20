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
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, ".") {
		return fmt.Errorf("invalid profile name %q: must be non-empty and contain no '.' or path separators", name)
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

	// AuthSource records how the stored credential was obtained, so status can
	// name a borrowed MCP-imported token instead of passing it off as a normal
	// OAuth login or stored key. Flag/env credentials aren't stored, so they're
	// resolved live and never written here. Empty on legacy profiles.
	AuthSource string `json:"auth_source,omitempty"` // AuthOrigin* constants

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

	// Provenance of the per-directory project binding (see ProjectFileName).
	// Like the env overrides it applies for a single invocation and is reverted
	// in Save, so a tree-local project is never baked into the profile on disk.
	dirProjectID envOverride

	// dirProjectErr holds a nearest .revenuecat.json that exists but could not
	// be read or parsed. Load records it instead of failing so a higher-
	// precedence override still bypasses it; ProjectBindingError surfaces it.
	dirProjectErr error

	// flagAPIKey holds an --api-key value for this invocation; never serialized.
	flagAPIKey string
}

// CredentialSource names where the active credential came from.
type CredentialSource string

const (
	SourceFlag    CredentialSource = "flag"
	SourceOAuth   CredentialSource = "oauth"
	SourceEnv     CredentialSource = "env"
	SourceProfile CredentialSource = "profile"
	SourceNone    CredentialSource = "none"
)

const (
	AuthOriginOAuthLogin = "oauth_login" // also set on signup
	AuthOriginAPIKey     = "api_key"
	AuthOriginMCPImport  = "mcp_import"
)

// Describe returns a human-readable phrase naming the source.
func (s CredentialSource) Describe() string {
	switch s {
	case SourceFlag:
		return "the --api-key flag"
	case SourceOAuth:
		return "the OAuth login in this profile"
	case SourceEnv:
		return "the RC_API_KEY environment variable"
	case SourceProfile:
		return "the API key stored in this profile"
	default:
		return "no credential"
	}
}

// envOverride records that Load replaced a field with an env-var value,
// keeping the pre-override on-disk value so Save can restore it rather than
// persist the ephemeral env value.
type envOverride struct {
	env, disk string
	set       bool
}

// BearerToken returns the auth credential for the Authorization: Bearer header.
func (c *Config) BearerToken() string {
	tok, _ := c.Credential()
	return tok
}

func (c *Config) Credential() (token string, source CredentialSource) {
	if c.flagAPIKey != "" {
		return c.flagAPIKey, SourceFlag
	}
	if c.TokenType == "oauth" && c.AccessToken != "" {
		return c.AccessToken, SourceOAuth
	}
	if c.envAPIKey.set && c.envAPIKey.env != "" {
		return c.envAPIKey.env, SourceEnv
	}
	if k := c.storedAPIKey(); k != "" {
		return k, SourceProfile
	}
	return "", SourceNone
}

// CredentialDescription refines Describe with stored provenance so an
// MCP-imported credential isn't named as an ordinary OAuth login or stored key.
func (c *Config) CredentialDescription() string {
	_, source := c.Credential()
	return c.DescribeSource(source)
}

// DescribeSource names a credential source, refining the stored credential's
// phrasing with its provenance (only flag/env overrides never carry one).
func (c *Config) DescribeSource(source CredentialSource) string {
	if c.AuthSource == AuthOriginMCPImport {
		switch source {
		case SourceOAuth:
			return "an MCP-imported access token (borrowed; no auto-refresh)"
		case SourceProfile:
			return "an MCP-imported API key"
		}
	}
	return source.Describe()
}

const (
	TokenValid      = "valid"
	TokenNearExpiry = "near_expiry"
	TokenExpired    = "expired"
	TokenNoExpiry   = "no_expiry" // borrowed tokens carry no expiry we can read
)

// TokenStatus is only meaningful when IsOAuth(); the near-expiry window matches
// NeedsRefresh's 5 minutes.
func (c *Config) TokenStatus() string {
	if c.TokenExpiresAt.IsZero() {
		return TokenNoExpiry
	}
	now := time.Now()
	switch {
	case now.After(c.TokenExpiresAt):
		return TokenExpired
	case c.TokenExpiresAt.Sub(now) <= 5*time.Minute:
		return TokenNearExpiry
	default:
		return TokenValid
	}
}

// CanAutoRefresh is false for borrowed MCP tokens, which carry no refresh token.
func (c *Config) CanAutoRefresh() bool {
	return c.IsOAuth() && c.RefreshToken != ""
}

// storedAPIKey is the API key on disk, ignoring any RC_API_KEY override.
func (c *Config) storedAPIKey() string {
	if c.envAPIKey.set {
		return c.envAPIKey.disk
	}
	return c.APIKey
}

// PresentCredentialSources lists every available credential source, highest precedence first.
func (c *Config) PresentCredentialSources() []CredentialSource {
	var s []CredentialSource
	if c.flagAPIKey != "" {
		s = append(s, SourceFlag)
	}
	if c.TokenType == "oauth" && c.AccessToken != "" {
		s = append(s, SourceOAuth)
	}
	if c.envAPIKey.set && c.envAPIKey.env != "" {
		s = append(s, SourceEnv)
	}
	if c.storedAPIKey() != "" {
		s = append(s, SourceProfile)
	}
	return s
}

func (c *Config) SetFlagAPIKey(key string) { c.flagAPIKey = key }

// SetAPIKey sets an explicit stored API key, clearing any flag or env override.
func (c *Config) SetAPIKey(key string) {
	c.APIKey = key
	c.flagAPIKey = ""
	c.envAPIKey = envOverride{}
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
	// tolerate a residual/invalid RC_PROFILE or .active pointer
	if env := os.Getenv("RC_PROFILE"); env != "" && validateProfileName(env) == nil {
		return env
	}
	if name := readActivePointer(); name != "" && validateProfileName(name) == nil {
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
		base := n[:len(n)-5]
		// dotted base = state file (<profile>.<name>.json), not a profile
		if strings.Contains(base, ".") {
			continue
		}
		names = append(names, base)
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

// Dir is the CLI's config/data directory — the single home for profiles,
// state, and other per-account files regardless of the working directory.
func Dir() (string, error) { return configDir() }

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

// ProjectFileName is a per-directory project binding, discovered by walking up
// from the working directory like git finding .git, so a repo can commit it.
const ProjectFileName = ".revenuecat.json"

type projectFile struct {
	ProjectID string `json:"project_id"`
}

// findProjectFile returns the nearest ProjectFileName walking up from start.
func findProjectFile(start string) (string, bool) {
	dir := start
	for {
		p := filepath.Join(dir, ProjectFileName)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func dirProjectID() (string, bool, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", false, nil
	}
	return dirProjectIDFrom(wd)
}

func dirProjectIDFrom(start string) (string, bool, error) {
	path, ok := findProjectFile(start)
	if !ok {
		return "", false, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("read %s: %w", path, err)
	}
	var f projectFile
	if err := json.Unmarshal(b, &f); err != nil {
		return "", false, fmt.Errorf("parse %s: %w", path, err)
	}
	if f.ProjectID == "" {
		return "", false, fmt.Errorf("%s: missing or empty project_id", path)
	}
	return f.ProjectID, true, nil
}

// Load reads a profile from disk, layering the per-directory project binding
// and env vars on top. The project ID resolves with this precedence (highest
// first): --project-id flag (applied by the caller) > RC_PROJECT_ID env >
// nearest .revenuecat.json in the directory tree > profile default.
// Missing files are not an error — they return a zero Config so first-run
// commands like `rc login` work cleanly.
// LoadStored reads a profile's config as written on disk, overlaying keyring
// secrets, without applying any per-invocation override (the per-directory
// binding, RC_* env vars, or --project-id). Use it to report a profile's own
// stored defaults, e.g. `profiles list`; use Load for the active resolved
// config a command should act on.
func LoadStored(profile string) (*Config, error) {
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
	return cfg, nil
}

func Load(profile string) (*Config, error) {
	cfg, err := LoadStored(profile)
	if err != nil {
		return nil, err
	}
	// The per-directory binding overrides the profile default but sits below
	// RC_PROJECT_ID and --project-id, which are applied after it. Recorded as an
	// override so Save reverts it rather than persisting a tree-local project.
	// A binding file that exists but can't be read or parsed is recorded, not
	// fatal: a higher-precedence override (RC_PROJECT_ID or --project-id) still
	// wins, and otherwise ProjectBindingError surfaces it rather than silently
	// falling through to another project.
	pid, ok, derr := dirProjectID()
	switch {
	case derr != nil:
		cfg.dirProjectErr = derr
	case ok:
		cfg.dirProjectID = envOverride{env: pid, disk: cfg.ProjectID, set: true}
		cfg.ProjectID = pid
	}
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

// UseProjectID records a project the user chose explicitly (projects use,
// projects create --use, the picker). It clears the env/dir project overrides
// so Save persists the choice even when it equals the injected override value —
// otherwise the revert would mistake the deliberate write for an untouched override.
func (c *Config) UseProjectID(id string) {
	c.ProjectID = id
	c.envProjectID.set = false
	c.dirProjectID.set = false
}

// StoredProjectID returns the profile's on-disk project default, unwinding any
// per-invocation overlay (the per-directory binding or RC_PROJECT_ID) that Load
// layered on top of it.
func (c *Config) StoredProjectID() string {
	if c.dirProjectID.set {
		return c.dirProjectID.disk
	}
	if c.envProjectID.set {
		return c.envProjectID.disk
	}
	return c.ProjectID
}

// ProjectBindingError reports a nearest .revenuecat.json that exists but could
// not be read or parsed. It returns nil when a higher-precedence override wins
// anyway — RC_PROJECT_ID (env) or an explicit --project-id (flagSet) — since
// those bypass the tree binding entirely. Otherwise the malformed file would
// silently retarget another project, so callers surface this before running.
func (c *Config) ProjectBindingError(flagSet bool) error {
	if c.dirProjectErr == nil || flagSet || c.envProjectID.set {
		return nil
	}
	return c.dirProjectErr
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
	// Order matters: the env override's disk value is the per-dir binding when
	// both applied, so unwind env first, then the per-dir binding, landing back
	// on the profile's own project.
	revert(cfg.envProjectID, &out.ProjectID)
	revert(cfg.dirProjectID, &out.ProjectID)
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
