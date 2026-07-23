package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/revenuecat/cli/internal/config"
)

// An imported MCP access token must never be refreshable: it has no refresh
// token, so NeedsRefresh must stay false even though it's an oauth token.
// This is the guarantee that keeps rc from rotation-revoking the MCP session.
func TestImportedToken_NeverRefreshes(t *testing.T) {
	cfg := &config.Config{}
	// Mirror what loginWithMCPCredential stores for an atk_ token.
	cfg.TokenType = "oauth"
	cfg.AccessToken = "atk_borrowed"
	cfg.RefreshToken = ""
	if cfg.NeedsRefresh() {
		t.Fatal("imported token with no refresh token must not report NeedsRefresh")
	}
	// And a durable sk_ import stays a plain API key (also never refreshes).
	cfg2 := &config.Config{APIKey: "sk_durable"}
	if cfg2.NeedsRefresh() {
		t.Fatal("sk_ key must not report NeedsRefresh")
	}
}

// discoverMCPCredentials pulls the RevenueCat bearer out of a Claude-style
// config and classifies atk_ (borrowed) vs sk_ (durable).
func TestDiscoverMCPCredentials_ClassifiesTokens(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("RC_FORCE_NPX_IMPORT", "1") // import is npx-only; force it on for the test
	claude := map[string]any{
		"mcpServers": map[string]any{
			"RevenueCat": map[string]any{
				"type": "http", "url": "https://mcp.revenuecat.ai/mcp",
				"headers": map[string]any{"Authorization": "Bearer atk_abc123"},
			},
			"Other": map[string]any{
				"type": "http", "url": "https://example.com/mcp",
				"headers": map[string]any{"Authorization": "Bearer nope"},
			},
		},
	}
	data, _ := json.Marshal(claude)
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	creds := discoverMCPCredentials()
	if len(creds) != 1 {
		t.Fatalf("expected 1 RevenueCat credential, got %d", len(creds))
	}
	if creds[0].Token != "atk_abc123" || creds[0].durable {
		t.Fatalf("atk_ should be non-durable: %+v", creds[0])
	}
}

// Off an npx run, MCP import is suppressed entirely — installed rc gets the
// durable login paths only.
func TestDiscoverMCPCredentials_SuppressedWhenNotNpx(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	// no RC_FORCE_NPX_IMPORT, and the test binary isn't under /_npx/
	claude := `{"mcpServers":{"RevenueCat":{"type":"http","url":"https://mcp.revenuecat.ai/mcp","headers":{"Authorization":"Bearer atk_abc123"}}}}`
	if err := os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(claude), 0o600); err != nil {
		t.Fatal(err)
	}
	if creds := discoverMCPCredentials(); len(creds) != 0 {
		t.Fatalf("import should be suppressed off npx, got %d creds", len(creds))
	}
}
