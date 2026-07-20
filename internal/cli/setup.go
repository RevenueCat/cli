package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/tui"
)

// agentClient is an AI agent CLI that rc setup can hand the onboarding to.
type agentClient struct {
	Name       string // display name
	Binary     string // executable looked up in PATH
	ToolkitKey string // skills-CLI agent identifier for the toolkit install
	LaunchArgs func(prompt string) []string
}

var agentClients = []agentClient{
	{"Claude Code", "claude", "claude-code", func(p string) []string { return []string{p} }},
	{"Codex", "codex", "codex", func(p string) []string { return []string{p} }},
	{"Cursor", "cursor-agent", "cursor", func(p string) []string { return []string{p} }},
	{"Gemini CLI", "gemini", "gemini-cli", func(p string) []string { return []string{"-i", p} }},
}

func newSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Set up RevenueCat for the app in this directory",
		Long: `Guided entry point for a new app: verifies this directory looks like an app
project, finds your installed AI agents, installs the RevenueCat AI Toolkit
skills, and launches the agent you pick with the full setup prompt. The agent
then creates the project, Test Store catalog, entitlement, offering, paywall,
and SDK integration, handing Apple sign-in back to you.

Prefer no agent? Choose "show me the prompt" to copy it manually, or follow
the step-by-step commands in the docs.`,
		Example: `  rc setup
  npx @revenuecat/cli setup`,
		Args: cobra.NoArgs,
		Annotations: map[string]string{
			"requires_human":        "true",
			"requires_human_reason": "launches the user's local AI agent in an interactive terminal; agents should follow the RevenueCat skills directly instead",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSetup(cmd)
		},
	}
	return cmd
}

func runSetup(cmd *cobra.Command) error {
	rt := RuntimeFrom(cmd.Context())
	if rt.Globals.NoInput || !tui.IsInteractive() {
		return errors.New("rc setup is interactive: it verifies the directory and launches your AI agent. Agents should use the RevenueCat skills directly (rc skills prompts --json)")
	}

	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	projectLabel, projectDetected := detectAppProject(dir)
	platform := platformFromLabel(projectLabel)
	agents := detectAgents()

	account := "not logged in"
	if rt.Config != nil && rt.Config.BearerToken() != "" {
		account = "logged in"
		if rt.Config.AccountEmail != "" {
			account = rt.Config.AccountEmail
		}
	}

	rt.Out.Title("RevenueCat setup — " + filepath.Base(dir))
	rt.Out.Lead("Hands your app's RevenueCat onboarding to an AI agent with RevenueCat's skills installed — nothing runs without your OK.")
	rt.Out.Field("Directory", collapseHome(dir), projectLabel)
	rt.Out.Field("Account", account)
	agentNames := make([]string, 0, len(agents))
	for _, a := range agents {
		agentNames = append(agentNames, a.Name)
	}
	if len(agentNames) > 0 {
		rt.Out.Field("Agents found", strings.Join(agentNames, ", "))
	} else {
		rt.Out.Field("Agents found", "none", "install Claude Code, Codex, Cursor, or Gemini CLI for agent-driven setup")
	}

	if rt.Config == nil || rt.Config.BearerToken() == "" {
		if err := setupAuthenticate(cmd, rt); err != nil {
			return err
		}
	}

	rt.Out.Info("Checking where this project stands…")
	stage := detectSetupStage(cmd, rt, platform)
	rt.Out.Field("Stage", stage.Label)

	if !projectDetected {
		cont, err := tui.ConfirmDefault(false, "No app project detected in this directory. Set up here anyway?", false)
		if err != nil {
			return err
		}
		if !cont {
			rt.Out.Info("Nothing changed.")
			rt.Out.Hint("cd into your app's root directory (the one with the Xcode project, package.json, or pubspec.yaml) and run rc setup again")
			return nil
		}
	}

	prompt := starterPromptByID(stage.PromptID) + setupToolingNote(rt)
	choice, err := pickSetupAgent(agents)
	if err != nil {
		return err
	}
	if choice == nil {
		rt.Out.Answer("Agent", "none — manual prompt")
		rt.Out.Blank()
		rt.Out.Info("Paste this into any agent after running rc skills install:")
		rt.Out.Info(prompt)
		rt.Out.Hint("more starter prompts:  rc skills prompts")
		return nil
	}
	rt.Out.Answer("Agent", choice.Name)

	rt.Out.Plan([]string{
		"Install/update the RevenueCat AI Toolkit skills for " + choice.Name,
		"Launch " + choice.Name + " with the \"" + stage.PromptID + "\" prompt (takes over this terminal)",
	})
	if err := confirmOrAbort(rt, "Launch "+choice.Name+" now?"); err != nil {
		return err
	}

	rt.Out.Info("Installing the RevenueCat AI Toolkit skills…")
	installArgs := []string{"--yes", "skills", "add", officialToolkitSource, "--global", "--agent", choice.ToolkitKey}
	installArgs = append(installArgs, "--skill")
	installArgs = append(installArgs, defaultToolkitSkills...)
	if err := (npxSkillsInstaller{}).Run(cmd, installArgs); err != nil {
		return fmt.Errorf("install skills: %w", err)
	}

	rt.Out.Info("Launching " + choice.Name + "…")
	rt.Out.Blank()
	agent := exec.CommandContext(cmd.Context(), choice.Binary, choice.LaunchArgs(prompt)...)
	agent.Stdin, agent.Stdout, agent.Stderr = os.Stdin, os.Stdout, os.Stderr
	return agent.Run()
}

// setupStage is where this project stands in the onboarding journey; it
// selects which starter prompt the launched agent receives, so re-running
// rc setup always hands over the NEXT stage rather than starting over.
type setupStage struct {
	Label    string // shown on the state block
	PromptID string // starter prompt handed to the agent
}

// detectSetupStage reads project state through the public API. Detection is
// deliberately local and cheap (three list calls); on any read error it
// falls back to the beginning, which is always safe because the skills
// themselves re-verify state.
// platformFromLabel maps the directory detection label to a platform hint so
// an Android-only project is never told to connect an Apple account.
func platformFromLabel(label string) string {
	switch {
	case strings.Contains(label, "Xcode"), strings.Contains(label, "Swift"):
		return "ios"
	case strings.Contains(label, "Android"):
		return "android"
	case strings.Contains(label, "Flutter"), strings.Contains(label, "React Native"), strings.Contains(label, "Expo"):
		return "cross"
	default:
		return "unknown"
	}
}

func detectSetupStage(cmd *cobra.Command, rt *Runtime, platform string) setupStage {
	fromNothing := setupStage{"new setup — nothing configured yet", "test-store-ready"}
	if rt.Config == nil || rt.Config.BearerToken() == "" {
		return fromNothing
	}
	// Read-only: use the bound project or nothing. Detection must never
	// prompt — a picker popping mid-"checking" would be exactly the
	// surprise this flow exists to avoid.
	projectID := rt.Config.ProjectID
	if projectID == "" {
		return setupStage{"logged in — no project selected (rc projects use <id> resumes an existing one)", "test-store-ready"}
	}
	client, err := rt.API()
	if err != nil {
		return fromNothing
	}
	ctx := cmd.Context()
	apps, err := client.Apps.List(ctx, projectID)
	if err != nil {
		return fromNothing
	}
	offerings, err := client.Offerings.List(ctx, projectID)
	if err != nil {
		return fromNothing
	}
	if len(offerings.Items) == 0 {
		return setupStage{"project exists — Test Store catalog not finished", "test-store-ready"}
	}
	var appStore, playStore *api.App
	for i := range apps.Items {
		switch apps.Items[i].Type {
		case "app_store":
			if appStore == nil {
				appStore = &apps.Items[i]
			}
		case "play_store":
			if playStore == nil {
				playStore = &apps.Items[i]
			}
		}
	}
	products, err := client.Products.List(ctx, projectID, nil)
	if err != nil {
		return fromNothing
	}
	hasProducts := func(app *api.App) bool {
		for _, p := range products.Items {
			if app != nil && p.AppID == app.ID {
				return true
			}
		}
		return false
	}

	// Apple applies when an App Store app exists, or none does but the
	// directory looks like an iOS/cross-platform app. Android-only projects
	// skip straight to the Play path.
	appleRelevant := appStore != nil || (playStore == nil && platform != "android")
	if appleRelevant {
		if appStore == nil || appStore.AppStore == nil ||
			!appStore.AppStore.SubscriptionKeyConfigured || !appStore.AppStore.AppStoreConnectAPIKeyConfigured {
			return setupStage{"Test Store ready — Apple account not connected", "connect-apple"}
		}
		if !hasProducts(appStore) {
			return setupStage{"Apple connected — App Store catalog not synced", "sync-store-catalog"}
		}
	}
	playRelevant := playStore != nil || platform == "android" || platform == "cross"
	if playRelevant {
		if playStore == nil {
			return setupStage{"Test Store ready — Play Store app not created (Play credentials are configured in the dashboard)", "sync-store-catalog"}
		}
		if !hasProducts(playStore) {
			return setupStage{"Play Store app exists — catalog not synced", "sync-store-catalog"}
		}
	}
	return setupStage{"store apps connected and catalogs synced", "check-project"}
}

// setupToolingNote pins the launched agent to the CLI: without it, agents
// with the RevenueCat MCP available reach for MCP tools even though the
// prompt came from the CLI.
func setupToolingNote(rt *Runtime) string {
	authed := ""
	if rt.Config != nil && rt.Config.BearerToken() != "" {
		authed = " and already authenticated"
	}
	return "\n\nTooling: use the `rc` CLI for every RevenueCat operation — it is installed" + authed +
		". Prefer it over the RevenueCat MCP and the dashboard for anything it supports; discover the entire command surface in ONE call with `rc commands --schemas` (do not run `rc schema` per command)."
}

// setupAuthenticate resolves auth before the terminal is handed to an agent:
// browser login and signup are human actions, and doing them mid-agent-run
// means fighting the agent for the terminal.
func setupAuthenticate(cmd *cobra.Command, rt *Runtime) error {
	const (
		optLogin = iota
		optSignup
		optSkip
	)
	choice := optLogin
	if err := tui.Form(false).
		Field(huh.NewSelect[int]().
			Title("You're not logged in. What would you like to do?").
			Options(
				huh.NewOption("Log in (opens your browser)", optLogin),
				huh.NewOption("Create a RevenueCat account", optSignup),
				huh.NewOption("Skip — the agent can handle it later", optSkip),
			).
			Value(&choice)).
		Run(); err != nil {
		return err
	}
	switch choice {
	case optLogin:
		if err := loginWithOAuth(cmd.Context(), rt); err != nil {
			return err
		}
	case optSignup:
		signup := newAuthSignupCmd()
		signup.SetContext(cmd.Context())
		if err := signup.RunE(signup, nil); err != nil {
			return err
		}
	default:
		rt.Out.Answer("Account", "skipped — the agent will sign you in or up")
		return nil
	}
	account := "logged in"
	if rt.Config != nil && rt.Config.AccountEmail != "" {
		account = rt.Config.AccountEmail
	}
	rt.Out.Answer("Account", account)
	return nil
}

func starterPromptByID(id string) string {
	for _, p := range revenueCatStarterPrompts() {
		if p.ID == id {
			return p.Prompt
		}
	}
	return "Use the create-revenuecat-project skill to set up RevenueCat for the app in this directory."
}

func pickSetupAgent(agents []agentClient) (*agentClient, error) {
	options := make([]huh.Option[int], 0, len(agents)+1)
	for i, a := range agents {
		options = append(options, huh.NewOption(a.Name, i))
	}
	options = append(options, huh.NewOption("None — just show me the prompt", -1))
	selected := 0
	if len(agents) == 0 {
		selected = -1
	}
	if err := tui.Form(false).
		Field(huh.NewSelect[int]().Title("Which agent should run the setup?").Options(options...).Value(&selected)).
		Run(); err != nil {
		return nil, err
	}
	if selected < 0 {
		return nil, nil
	}
	return &agents[selected], nil
}

func detectAgents() []agentClient {
	found := make([]agentClient, 0, len(agentClients))
	for _, a := range agentClients {
		if _, err := exec.LookPath(a.Binary); err == nil {
			found = append(found, a)
		}
	}
	return found
}

// detectAppProject decides whether dir looks like an app project root and
// names what it found. The label rides the Directory field so a wrong-cwd
// mistake is visible before anything runs.
func detectAppProject(dir string) (label string, ok bool) {
	if home, err := os.UserHomeDir(); err == nil && dir == home {
		return "this is your home directory, not an app", false
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "*.xcodeproj")); len(matches) > 0 {
		return "Xcode project (" + filepath.Base(matches[0]) + ")", true
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "*.xcworkspace")); len(matches) > 0 {
		return "Xcode workspace (" + filepath.Base(matches[0]) + ")", true
	}
	if _, err := os.Stat(filepath.Join(dir, "pubspec.yaml")); err == nil {
		return "Flutter app", true
	}
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		switch {
		case strings.Contains(string(data), `"react-native"`):
			return "React Native app", true
		case strings.Contains(string(data), `"expo"`):
			return "Expo app", true
		default:
			return "JavaScript project", true
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "Package.swift")); err == nil {
		return "Swift package", true
	}
	for _, gradle := range []string{"settings.gradle", "settings.gradle.kts", "build.gradle", "build.gradle.kts"} {
		if _, err := os.Stat(filepath.Join(dir, gradle)); err == nil {
			return "Android project", true
		}
	}
	return "no app project detected", false
}

func collapseHome(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(dir, home) {
		return "~" + strings.TrimPrefix(dir, home)
	}
	return dir
}
