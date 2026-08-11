package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

const revenueCatMCPURL = "https://mcp.revenuecat.ai/mcp"

var (
	pbxprojBundleIDPattern = regexp.MustCompile(`PRODUCT_BUNDLE_IDENTIFIER = ([A-Za-z0-9.\-]+);`)
	tuistBundleIDPattern   = regexp.MustCompile(`bundleId:\s*"([A-Za-z0-9.\-]+)"`)
)

// detectBundleID pulls the app's bundle identifier out of the Xcode or Tuist
// project so the App Store app record can be created without asking.
func detectBundleID(dir string) string {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.xcodeproj", "project.pbxproj"))
	for _, path := range matches {
		if id := firstBundleID(path, pbxprojBundleIDPattern); id != "" {
			return id
		}
	}
	if id := firstBundleID(filepath.Join(dir, "Project.swift"), tuistBundleIDPattern); id != "" {
		return id
	}
	return ""
}

func firstBundleID(path string, pattern *regexp.Regexp) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, m := range pattern.FindAllStringSubmatch(string(data), -1) {
		id := m[1]
		if strings.HasSuffix(id, "Tests") || strings.HasSuffix(id, "UITests") {
			continue
		}
		return id
	}
	return ""
}

// configureAgentMCP registers the RevenueCat MCP server for the chosen agent
// with the CLI's credential, so the agent has authenticated MCP access from
// its first turn. mcp.revenuecat.ai accepts both OAuth tokens and v2 secret
// API keys as Bearer credentials.
func configureAgentMCP(cmd *cobra.Command, rt *Runtime, agent *agentClient) {
	token := ""
	if rt.Config != nil {
		token = rt.Config.BearerToken()
	}
	switch agent.Binary {
	case "claude":
		// Idempotent refresh: if OUR server entry exists (matched by URL),
		// remove and re-add so the Bearer credential is always current —
		// a stale token is worse than none (silent 401s mid-run). An entry
		// with a different URL is the user's own config; leave it alone.
		existing, _ := exec.CommandContext(cmd.Context(), "claude", "mcp", "get", "RevenueCat").CombinedOutput()
		if strings.Contains(string(existing), "mcp.revenuecat.ai") {
			_ = exec.CommandContext(cmd.Context(), "claude", "mcp", "remove", "--scope", "user", "RevenueCat").Run()
		} else if strings.Contains(string(existing), "http") {
			rt.Out.Info("A RevenueCat MCP entry with a custom URL already exists in Claude Code — leaving it untouched")
			return
		}
		args := []string{"mcp", "add", "--scope", "user", "--transport", "http", "RevenueCat", revenueCatMCPURL}
		if token != "" {
			args = append(args, "--header", "Authorization: Bearer "+token)
		}
		run := exec.CommandContext(cmd.Context(), "claude", args...)
		if out, err := run.CombinedOutput(); err != nil {
			rt.Out.Warn("Couldn't configure the RevenueCat MCP for Claude Code: " + strings.TrimSpace(string(out)))
			return
		}
		rt.Out.Info("RevenueCat MCP configured for Claude Code (authenticated with your rc credential)")
	case "codex":
		// Codex owns its own OAuth store, so registration and auth are
		// separate: register if missing, then hint login only when needed.
		list, _ := exec.CommandContext(cmd.Context(), "codex", "mcp", "list").CombinedOutput()
		if strings.Contains(string(list), "RevenueCat") {
			rt.Out.Info("RevenueCat MCP already registered for Codex")
			rt.Out.Hint("if it shows as not logged in:  codex mcp login RevenueCat")
			return
		}
		run := exec.CommandContext(cmd.Context(), "codex", "mcp", "add", "RevenueCat", "--url", revenueCatMCPURL)
		if out, err := run.CombinedOutput(); err != nil {
			rt.Out.Warn("Couldn't register the RevenueCat MCP for Codex: " + strings.TrimSpace(string(out)))
			return
		}
		rt.Out.Info("RevenueCat MCP registered for Codex")
		rt.Out.Hint("authenticate it once with:  codex mcp login RevenueCat")
	default:
		rt.Out.Hint("connect the RevenueCat MCP in " + agent.Name + " settings:  " + revenueCatMCPURL)
	}
}
