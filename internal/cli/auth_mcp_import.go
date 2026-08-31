package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/revenuecat/cli/internal/config"
)

// Importing an existing RevenueCat MCP credential lets a dev who already has
// the MCP working skip the browser round-trip. The token lives in plaintext
// in the agent's MCP config (an http MCP server carries auth in a header), so
// we can read it. Two kinds:
//
//   - sk_… : a v2 secret key. Durable, no expiry. Ideal.
//   - atk_…: an OAuth access token. The backend issues these with a 1-hour TTL
//     and the config carries NO refresh
//     token — that lives in the client's private store. So an imported atk_ is
//     a fast start, not a durable session: it works until it expires and rc
//     can't refresh it. The token is opaque, so we can't read its remaining
//     life; we validate by actually calling the API at import.

type mcpCredential struct {
	Source  string // "Claude Code", "Cursor"
	Token   string
	durable bool // sk_ key (no expiry) vs atk_ (1h, no refresh)
}

// runningViaNpx reports whether this process was launched by `npx` (the binary
// runs out of npm's _npx cache) rather than an installed rc (brew, npm -g,
// release tarball). Importing a borrowed 1-hour MCP token is a throwaway-session
// convenience, so it's only offered on npx runs; an installed rc should log in
// properly. RC_FORCE_NPX_IMPORT=1 overrides for testing.
func runningViaNpx() bool {
	if os.Getenv("RC_FORCE_NPX_IMPORT") == "1" {
		return true
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.Contains(filepath.ToSlash(exe), "/_npx/")
}

func discoverMCPCredentials() []mcpCredential {
	// Only surface MCP-imported credentials for throwaway npx sessions; an
	// installed rc gets the durable login paths only.
	if !runningViaNpx() {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var creds []mcpCredential
	seen := map[string]bool{}
	add := func(source, header string) {
		token := strings.TrimSpace(header)
		token = strings.TrimPrefix(token, "Bearer ")
		token = strings.TrimPrefix(token, "bearer ")
		if token == "" || seen[token] {
			return
		}
		seen[token] = true
		creds = append(creds, mcpCredential{
			Source:  source,
			Token:   token,
			durable: strings.HasPrefix(token, "sk_"),
		})
	}
	for _, tok := range revenueCatTokensFromJSON(filepath.Join(home, ".claude.json")) {
		add("Claude Code", tok)
	}
	for _, tok := range revenueCatTokensFromJSON(filepath.Join(home, ".cursor", "mcp.json")) {
		add("Cursor", tok)
	}
	return creds
}

// revenueCatTokensFromJSON walks a JSON file for mcpServers entries whose URL
// is the RevenueCat MCP and returns their Authorization header value.
func revenueCatTokensFromJSON(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil
	}
	var out []string
	var walk func(any)
	walk = func(node any) {
		switch n := node.(type) {
		case map[string]any:
			if servers, ok := n["mcpServers"].(map[string]any); ok {
				for _, cfg := range servers {
					cm, ok := cfg.(map[string]any)
					if !ok {
						continue
					}
					if url, _ := cm["url"].(string); !strings.Contains(url, "mcp.revenuecat.ai") {
						continue
					}
					if headers, ok := cm["headers"].(map[string]any); ok {
						if auth, ok := headers["Authorization"].(string); ok {
							out = append(out, auth)
						}
					}
				}
			}
			for _, v := range n {
				walk(v)
			}
		case []any:
			for _, v := range n {
				walk(v)
			}
		}
	}
	walk(root)
	return out
}

func (c mcpCredential) label() string {
	if c.durable {
		return fmt.Sprintf("Use the RevenueCat key from your %s config  (stays logged in)", c.Source)
	}
	return fmt.Sprintf("Use the RevenueCat token from your %s config  (quick start; expires within the hour, no auto-refresh)", c.Source)
}

// loginWithMCPCredential stores an imported credential the same way its native
// path would (sk_ as an API key, atk_ as an oauth access token) then validates
// it with a real call via loginWithAPIKey/finishLogin. A dead atk_ fails that
// call, and we translate it into a browser-login nudge.
func loginWithMCPCredential(ctx context.Context, rt *Runtime, cred mcpCredential) error {
	if cred.durable {
		return loginWithAPIKeyOrigin(ctx, rt, cred.Token, config.AuthOriginMCPImport)
	}
	// No refresh token and no expiry on purpose: this is a borrowed access
	// token. Empty RefreshToken makes Config.NeedsRefresh() return false, so
	// rc never tries (and never could) to refresh it — which also means it
	// can't rotation-revoke the MCP's own session.
	rt.Config.SetOAuthTokens(cred.Token, "", time.Time{})
	rt.Config.AuthSource = config.AuthOriginMCPImport
	rt.Config.AccountEmail = ""
	rt.Config.AccountName = ""
	rt.client = nil
	client, err := rt.API()
	if err != nil {
		return err
	}
	// Validate before clearing or saving: a dead token fails here instead of
	// faking a "Logged in" that breaks on first use, and a failed import won't
	// announce a project clear that never saved.
	if _, err := client.Projects.List(ctx); err != nil {
		rt.Out.Hint("Run `rc login` and choose browser login instead.")
		return fmt.Errorf("that %s token didn't work (it may have already expired — they last about an hour): %w", cred.Source, err)
	}
	// Token is good — now safe to clear a stale project binding and persist.
	clearProjectBinding(rt)
	if err := finishLogin(ctx, rt, client); err != nil {
		return err
	}
	rt.Out.Hint("This token expires within the hour and can't refresh. Run `rc login` (browser) when you want a session that stays logged in.")
	return nil
}
