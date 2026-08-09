package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/tui"
)

// One-shot preparation: everything that would interrupt the agent mid-run is
// a human/auth action, and those happen BEFORE the terminal is handed over —
// RevenueCat login (already handled), Apple sign-in with 2FA, and MCP
// authentication for the chosen agent.

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

// offerApple optionally connects the Apple account for the real App Store.
// Apple sign-in is a human 2FA action the launched agent can't perform, so it
// runs here, before the handoff. Creates the project and App Store app records
// if missing, then runs the guided Apple setup inline.
func offerApple(cmd *cobra.Command, rt *Runtime, dir, platform string) bool {
	if platform != "ios" && platform != "cross" {
		return false
	}
	if rt.Config == nil || rt.Config.BearerToken() == "" {
		return false
	}
	rt.Out.Info("Connecting Apple lets the agent set up the real App Store too — it signs in to your Apple account (2FA) and creates App Store Connect keys. Skip it and the agent builds everything on the Test Store; connect Apple later with rc apps apple setup.")
	ok, err := tui.ConfirmDefault(false, "Connect your Apple account now?", false)
	if err != nil || !ok {
		return false
	}

	client, err := rt.API()
	if err != nil {
		rt.Out.Warn("Couldn't reach RevenueCat (" + err.Error() + ") — the agent will handle Apple later.")
		return false
	}
	ctx := cmd.Context()

	projectID := rt.Config.ProjectID
	if projectID == "" {
		name := filepath.Base(dir)
		if err := tui.Form(false).
			Field(huh.NewInput().Title("Project name").Value(&name).Validate(tui.Required("name"))).
			Run(); err != nil {
			return false
		}
		project, err := client.Projects.Create(ctx, api.ProjectCreate{Name: name})
		if err != nil {
			rt.Out.Warn("Couldn't create the project (" + err.Error() + ") — the agent will handle it.")
			return false
		}
		projectID = project.ID
		rt.Config.ProjectID = projectID
		if err := config.Save(rt.Globals.Profile, rt.Config); err != nil {
			rt.Out.Info(fmt.Sprintf("note: couldn't save profile: %v", err))
		}
		rt.Out.Answer("Project", name+"  ("+projectID+")")
	}

	appID, appName := "", ""
	if apps, err := client.Apps.List(ctx, projectID); err == nil {
		for _, app := range apps.Items {
			if app.Type == "app_store" {
				appID = app.ID
				break
			}
		}
	}
	if appID == "" {
		bundleID := detectBundleID(dir)
		if err := tui.Form(false).
			Field(huh.NewInput().Title("Bundle ID").Description("detected from the Xcode project").Value(&bundleID).Validate(tui.Required("bundle ID"))).
			Run(); err != nil {
			return false
		}
		app, err := client.Apps.Create(ctx, projectID, api.AppCreate{
			Name:     filepath.Base(dir) + " (App Store)",
			Type:     "app_store",
			AppStore: &api.AppStoreAppConfig{BundleID: bundleID},
		})
		if err != nil {
			rt.Out.Warn("Couldn't create the App Store app (" + err.Error() + ") — the agent will handle it.")
			return false
		}
		appID, appName = app.ID, app.Name
		rt.Out.Answer("App Store app", appName+"  ("+bundleID+")")
	}

	rt.Out.Blank()
	apple := newAppsAppleCmd()
	setupSub, _, err := apple.Find([]string{"setup"})
	if err != nil {
		return false
	}
	setupSub.SetContext(cmd.Context())
	if err := setupSub.RunE(setupSub, []string{appID}); err != nil {
		rt.Out.Warn("Apple setup didn't finish (" + err.Error() + ") — rerun with: rc apps apple setup " + appID)
		return false
	}
	return true
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
