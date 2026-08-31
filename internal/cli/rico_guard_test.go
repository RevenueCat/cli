package cli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/cli"
	"github.com/revenuecat/cli/internal/config"
)

// runRicoList invokes `rc rico conversations list` without the shared helper,
// which force-clears RC_API_KEY and would defeat the env-override cases.
func runRicoList(t *testing.T, configDir string) error {
	t.Helper()
	t.Setenv("RC_CONFIG_DIR", configDir)
	t.Setenv("RC_PROFILE", "")
	t.Setenv("RC_PROJECT_ID", "")
	t.Setenv("RC_BASE_URL", "")
	// Hermetic backstop: if the guard ever fails to fire, the request hits a
	// closed local port instead of the real backend.
	t.Setenv("RC_RICO_BASE_URL", "http://127.0.0.1:1")

	var out, errb bytes.Buffer
	root := cli.NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs([]string{"rico", "conversations", "list", "--json", "--no-input"})
	return root.ExecuteContext(context.Background())
}

// Rico's backend rejects secret API keys, so the CLI fails fast with the
// remedy instead of surfacing the server's opaque 401.
func TestRico_SecretKeyFailsFastWithRemedy(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", configDir)
	t.Setenv("RC_API_KEY", "")
	if err := config.Save("", &config.Config{APIKey: "sk_stored", ProjectID: "proj_x"}); err != nil {
		t.Fatal(err)
	}

	err := runRicoList(t, configDir)
	if err == nil || !strings.Contains(err.Error(), "rc login") {
		t.Fatalf("want a fail-fast login remedy for an sk_ credential, got: %v", err)
	}

	// When RC_API_KEY shadows a working login, the remedy is to unset it.
	if err := config.Save("", &config.Config{TokenType: "oauth", AccessToken: "atk_live", ProjectID: "proj_x"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RC_API_KEY", "sk_env")
	err = runRicoList(t, configDir)
	if err == nil || !strings.Contains(err.Error(), "unset RC_API_KEY") {
		t.Fatalf("want the unset-RC_API_KEY remedy when a login is shadowed, got: %v", err)
	}
}
