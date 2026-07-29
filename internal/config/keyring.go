package config

import (
	"encoding/json"
	"path/filepath"

	"github.com/zalando/go-keyring"
)

// Secrets are kept in the OS credential store (macOS Keychain, Windows
// Credential Manager, Linux Secret Service) instead of the plaintext profile
// file. go-keyring is pure Go (no CGO), so this works in our static binaries.
// When the keyring is unavailable — headless Linux, no D-Bus, Docker, a locked
// store — we fall back to writing the secrets into the 0600 file, matching the
// GitHub CLI's keychain-with-fallback model.

const keyringService = "revenuecat-cli"

// keyringKey namespaces the credential entry by config dir, not profile name
// alone. The profile *file* already lives under configDir, so the keyring entry
// must share that scope — otherwise two config dirs using the same profile name
// (every temp-dir test uses "default") collide on one global keyring entry.
// Falls back to the bare profile name if the config dir can't be resolved.
func keyringKey(profile string) string {
	name := ProfileName(profile)
	dir, err := configDir()
	if err != nil {
		return name
	}
	return filepath.Join(dir, name)
}

type storedSecrets struct {
	APIKey       string `json:"api_key,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

// storeSecrets writes the config's credentials to the OS keyring under the
// profile name. Returns true if the keyring accepted them (caller then blanks
// the file copy); false means the keyring is unavailable and the caller must
// keep the secrets in the file.
func storeSecrets(profile string, cfg *Config) bool {
	key := keyringKey(profile)
	s := storedSecrets{APIKey: cfg.APIKey, AccessToken: cfg.AccessToken, RefreshToken: cfg.RefreshToken}
	if s == (storedSecrets{}) {
		// Nothing to store (e.g. after logout) — clear any prior entry.
		_ = keyring.Delete(keyringService, key)
		return true
	}
	blob, err := json.Marshal(s)
	if err != nil {
		return false
	}
	if err := keyring.Set(keyringService, key, string(blob)); err != nil {
		return false
	}
	return true
}

// loadSecrets overlays keyring-stored credentials onto cfg. A missing entry or
// unavailable keyring is not an error — the file values (if any) stand.
func loadSecrets(profile string, cfg *Config) {
	blob, err := keyring.Get(keyringService, keyringKey(profile))
	if err != nil || blob == "" {
		return
	}
	var s storedSecrets
	if json.Unmarshal([]byte(blob), &s) != nil {
		return
	}
	if s.APIKey != "" {
		cfg.APIKey = s.APIKey
	}
	if s.AccessToken != "" {
		cfg.AccessToken = s.AccessToken
	}
	if s.RefreshToken != "" {
		cfg.RefreshToken = s.RefreshToken
	}
}
