package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/revenuecat/cli/internal/cli"
	"github.com/revenuecat/cli/internal/config"
)

func seedProfile(t *testing.T, dir string, cfg *config.Config) {
	t.Helper()
	t.Setenv("RC_CONFIG_DIR", dir)
	t.Setenv("RC_PROFILE", "")
	if err := config.Save("", cfg); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
}

// statusData runs `auth status --json` in dir; no project is configured, so
// status makes no network call.
func statusData(t *testing.T, dir string, extraArgs ...string) map[string]any {
	t.Helper()
	t.Setenv("RC_CONFIG_DIR", dir)
	t.Setenv("RC_PROFILE", "")
	t.Setenv("RC_PROJECT_ID", "")
	t.Setenv("RC_BASE_URL", "")

	args := append([]string{"auth", "status", "--json", "--no-input"}, extraArgs...)
	var out, errb bytes.Buffer
	root := cli.NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(args)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("auth status failed: %v\nstderr: %s", err, errb.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("status not JSON: %v\n%s", err, out.String())
	}
	return env.Data
}

func wantField(t *testing.T, data map[string]any, key string, want any) {
	t.Helper()
	if got := data[key]; got != want {
		t.Errorf("%s = %v (%T); want %v", key, got, data[key], want)
	}
}

func TestAuthStatus_NamesOAuthLogin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_API_KEY", "")
	seedProfile(t, dir, &config.Config{
		TokenType:      "oauth",
		AccessToken:    "atk_live",
		RefreshToken:   "rt_x",
		TokenExpiresAt: time.Now().Add(time.Hour),
		AuthSource:     config.AuthOriginOAuthLogin,
		AccountEmail:   "dev@example.com",
	})

	data := statusData(t, dir)
	wantField(t, data, "credential_source", "oauth")
	wantField(t, data, "credential_description", "the OAuth login in this profile")
	wantField(t, data, "auth_origin", "oauth_login")
	wantField(t, data, "token_status", "valid")
	wantField(t, data, "token_can_refresh", true)
}

func TestAuthStatus_NamesAPIKeyFlag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_API_KEY", "")
	seedProfile(t, dir, &config.Config{APIKey: "sk_stored", AuthSource: config.AuthOriginAPIKey})

	data := statusData(t, dir, "--api-key", "sk_flag")
	wantField(t, data, "credential_source", "flag")
	wantField(t, data, "credential_description", "the --api-key flag")
}

func TestAuthStatus_OmitsAuthOriginWhenOverridden(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_API_KEY", "")
	seedProfile(t, dir, &config.Config{APIKey: "sk_from_mcp", AuthSource: config.AuthOriginMCPImport})

	// A live --api-key wins, so the stored mcp_import provenance is not in use.
	data := statusData(t, dir, "--api-key", "sk_flag")
	wantField(t, data, "credential_source", "flag")
	wantField(t, data, "auth_origin", nil)
}

func TestAuthStatus_NamesEnvVar(t *testing.T) {
	dir := t.TempDir()
	seedProfile(t, dir, &config.Config{})
	t.Setenv("RC_API_KEY", "sk_env")

	data := statusData(t, dir)
	wantField(t, data, "credential_source", "env")
	wantField(t, data, "credential_description", "the RC_API_KEY environment variable")
}

func TestAuthStatus_NamesProfileKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_API_KEY", "")
	seedProfile(t, dir, &config.Config{APIKey: "sk_stored", AuthSource: config.AuthOriginAPIKey})

	data := statusData(t, dir)
	wantField(t, data, "credential_source", "profile")
	wantField(t, data, "credential_description", "the API key stored in this profile")
	wantField(t, data, "auth_origin", "api_key")
}

func TestAuthStatus_NamesMCPImportedAccessToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_API_KEY", "")
	// Mirrors what loginWithMCPCredential stores for a borrowed atk_ token.
	seedProfile(t, dir, &config.Config{
		TokenType:   "oauth",
		AccessToken: "atk_borrowed",
		AuthSource:  config.AuthOriginMCPImport,
	})

	data := statusData(t, dir)
	wantField(t, data, "credential_source", "oauth")
	wantField(t, data, "credential_description", "an MCP-imported access token (borrowed; no auto-refresh)")
	wantField(t, data, "auth_origin", "mcp_import")
	wantField(t, data, "token_status", "no_expiry")
	wantField(t, data, "token_can_refresh", false)
}

func TestAuthStatus_NamesMCPImportedAPIKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RC_API_KEY", "")
	seedProfile(t, dir, &config.Config{APIKey: "sk_from_mcp", AuthSource: config.AuthOriginMCPImport})

	data := statusData(t, dir)
	wantField(t, data, "credential_source", "profile")
	wantField(t, data, "credential_description", "an MCP-imported API key")
	wantField(t, data, "auth_origin", "mcp_import")
}

// An RC_API_KEY set alongside an OAuth login must not silently take over.
func TestAuthStatus_ConflictNamesActiveAndIgnored(t *testing.T) {
	dir := t.TempDir()
	seedProfile(t, dir, &config.Config{
		TokenType:      "oauth",
		AccessToken:    "atk_live",
		RefreshToken:   "rt_x",
		TokenExpiresAt: time.Now().Add(time.Hour),
		AuthSource:     config.AuthOriginOAuthLogin,
	})
	t.Setenv("RC_API_KEY", "sk_env")

	data := statusData(t, dir)
	wantField(t, data, "credential_source", "oauth")
	conflict, ok := data["credential_conflict"].(map[string]any)
	if !ok {
		t.Fatalf("expected credential_conflict, got %v", data["credential_conflict"])
	}
	wantField(t, conflict, "active_source", "oauth")
	ignored, _ := conflict["ignored_sources"].([]any)
	found := false
	for _, s := range ignored {
		if s == "env" {
			found = true
		}
	}
	if !found {
		t.Errorf("ignored_sources should name the env key, got %v", ignored)
	}
}
