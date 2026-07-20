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
	agents := detectAgents()

	account := "not logged in  (the agent can create an account or sign you in)"
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

	prompt := setupPrompt()
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
		"Launch " + choice.Name + " with the setup prompt (takes over this terminal)",
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

// setupPrompt is the full-journey prompt: the same contract as the
// test-store-ready starter prompt, which the toolkit skills are written
// against.
func setupPrompt() string {
	for _, p := range revenueCatStarterPrompts() {
		if p.ID == "test-store-ready" {
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
