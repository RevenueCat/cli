package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

const (
	officialToolkitSource = "RevenueCat/ai-toolkit"
	officialToolkitDocs   = "https://www.revenuecat.com/docs/tools/overview"
	projectSkillTrigger   = "Use the create-revenuecat-project skill to make this app RevenueCat Test Store-ready, then report every later production-store stage separately."
)

var defaultToolkitSkills = []string{
	"create-revenuecat-project",
	"integrate-revenuecat",
	"revenuecat-paywall",
	"revenuecat-store-state",
}

var defaultToolkitAgents = []string{
	"claude-code",
	"codex",
	"cursor",
	"gemini-cli",
	"github-copilot",
}

type skillsInstaller interface {
	Run(*cobra.Command, []string) error
}

type starterPrompt struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
}

func revenueCatStarterPrompts() []starterPrompt {
	return []starterPrompt{
		{
			ID:     "test-store-ready",
			Title:  "Make this app Test Store-ready",
			Prompt: "Use the create-revenuecat-project skill to inspect the app in this directory, create my RevenueCat account if needed, and make this app Test Store-ready. The outcome is a user experience, not a test assertion: a user in a normal dev run can find an Upgrade entry in the app's navigation, open the RevenueCat paywall, complete a purchase, and see the entitlement unlock. Acceptance: build and install the app, then hand ME a 30-second checklist to tap through (launch, find Upgrade, purchase, confirm unlock) and wait for my confirmation — do not puppet the simulator UI yourself, and do not claim completion from CLI or log evidence alone. Build whatever that requires: project, Test Store products and prices, entitlement, offering and packages, dashboard paywall, Purchases and RevenueCatUI dependencies, and app code. Configure RevenueCat with the test_ key on every normal dev launch — the test_ key decides WHICH environment, never WHETHER to configure; do not gate configure() behind debug or test flags. Ask before accepting legal terms and report any incomplete stage explicitly.",
		},
		{
			ID:     "connect-apple",
			Title:  "Connect my Apple account",
			Prompt: "Continue this app's RevenueCat setup with the Apple stage of the create-revenuecat-project skill. Verify the App Store app and bundle ID, run the read-only Apple check first, then give me the local interactive rc apps apple setup command for Apple sign-in and 2FA. Verify the missing In-App Purchase and App Store Connect keys are configured without asking me to paste Apple credentials into chat.",
		},
		{
			ID:     "sync-store-catalog",
			Title:  "Sync my product catalog to the stores",
			Prompt: "Use the revenuecat-store-state skill to create a persisted plan for this project's store products matching its verified Test Store catalog — App Store Connect (subscription groups, prices, availability, localizations) and/or Google Play (base plans, prices, availability), depending on which store apps the project has. For Play, service credentials must already be configured in the RevenueCat dashboard — check first and hand me the dashboard steps if missing. Show me the exact plan and wait for approval before applying that same plan ID. After apply, attach the store products to the existing RevenueCat entitlement and packages, configure the release platform key separately from debug, and report store sandbox verification separately.",
		},
		{
			ID:     "integrate-sdk",
			Title:  "Integrate only the RevenueCat SDK",
			Prompt: "Use the integrate-revenuecat skill to inspect this app, install the correct Purchases dependencies, configure debug with the Test Store key and release with the platform key, build both paths, and verify offerings load. Do not create dashboard resources unless I ask.",
		},
		{
			ID:     "check-project",
			Title:  "Check my RevenueCat setup",
			Prompt: "Use the revenuecat-status skill to audit my RevenueCat project, identify missing or inconsistent configuration, and give me exact recovery steps without changing anything first.",
		},
	}
}

func showStarterPrompts(rt *Runtime) {
	rt.Out.Info("Copy one of these starter prompts into a new agent session:")
	for _, item := range revenueCatStarterPrompts() {
		rt.Out.Info(fmt.Sprintf("%s\n  %s", item.Title, item.Prompt))
	}
}

type npxSkillsInstaller struct{}

func (npxSkillsInstaller) Run(cmd *cobra.Command, args []string) error {
	path, err := exec.LookPath("npx")
	if err != nil {
		return fmt.Errorf("npx is required to install the RevenueCat AI Toolkit; install Node.js or follow %s: %w", officialToolkitDocs, err)
	}
	child := exec.CommandContext(cmd.Context(), path, args...)
	child.Stdout = cmd.ErrOrStderr()
	child.Stderr = cmd.ErrOrStderr()
	child.Stdin = cmd.InOrStdin()
	if skillsInstallIsGlobal(args) {
		dir, err := os.MkdirTemp("", "rc-skills-install-")
		if err != nil {
			return fmt.Errorf("create isolated skills install directory: %w", err)
		}
		defer os.RemoveAll(dir)
		child.Dir = dir
	}
	return child.Run()
}

func skillsInstallIsGlobal(args []string) bool {
	for _, arg := range args {
		if arg == "--global" {
			return true
		}
	}
	return false
}

func newSkillsCmd() *cobra.Command {
	return newSkillsCmdWithInstaller(npxSkillsInstaller{})
}

func newSkillsCmdWithInstaller(installer skillsInstaller) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "skills",
		Aliases: []string{"skill"},
		Short:   "Manage the official RevenueCat AI Toolkit",
		Long: `RevenueCat's official AI Toolkit provides maintained agent workflows for
project setup, SDK integration, catalog management, and project health checks.

rc delegates to the standard Skills CLI instead of embedding a stale copy of
those workflows. Marketplace installation options for Codex, Claude Code,
Cursor, VS Code, and Gemini are documented on the RevenueCat website.

After installation, start a new agent session or reload the agent. Skills run
when a request matches their description; they do not run during installation.
Name a skill explicitly when you want predictable selection.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			if !rt.Out.IsJSON() {
				return cmd.Help()
			}
			return rt.Out.Render(map[string]any{
				"source":                     officialToolkitSource,
				"install_command":            "rc skills install",
				"underlying_install_command": "npx skills add " + officialToolkitSource + " --global",
				"prompts_command":            "rc skills prompts",
				"docs_url":                   officialToolkitDocs,
			})
		},
	}
	cmd.AddCommand(newSkillsInstallCmd(installer), newSkillsPromptsCmd())
	return cmd
}

func newSkillsPromptsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prompts",
		Short: "Show copy-ready prompts for common RevenueCat workflows",
		Long: `Shows starter prompts that explicitly trigger the maintained RevenueCat
skills. Copy one into a new agent session after installing or updating the
toolkit with rc skills install. Use --json to render selectable prompts in an
agent UI.`,
		Example: `  rc skills prompts
  rc skills prompts --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			if !rt.Out.IsJSON() {
				showStarterPrompts(rt)
				return nil
			}
			return rt.Out.Render(map[string]any{
				"starter_prompts": revenueCatStarterPrompts(),
			})
		},
	}
}

func newSkillsInstallCmd(installer skillsInstaller) *cobra.Command {
	var global, project, copyFiles, all bool
	var branch string
	var agents, skills []string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Pull and install skills from RevenueCat/ai-toolkit",
		Long: `Pulls the current official RevenueCat skills globally by delegating to:

  npx skills add RevenueCat/ai-toolkit --global

The standard Skills CLI detects supported agents and owns installation paths,
lock files, security review, and updates. This command does not vendor or cache
a separate copy of the toolkit inside rc. Pass --project to install into the
current repository instead. The default installs the four skills needed for
complete project setup for RevenueCat's supported agent clients—Claude Code,
Codex, Cursor, Gemini CLI, and GitHub Copilot/VS Code—without opening the
underlying agent or skill pickers. Pass --agent to override the targets or
--all to install every RevenueCat skill. Under --no-input, pass --yes.

After installing or updating, start a new agent session or reload the agent so
it discovers the latest skills. Then ask naturally for RevenueCat setup or name
the create-revenuecat-project skill explicitly.

Set RC_SKILLS_BRANCH to install an unreleased branch for testing. --branch
overrides the environment variable.`,
		Example: `  rc skills install
  rc skills install --project
  RC_SKILLS_BRANCH=rc-cli-project-setup-workflows rc skills install
  rc skills install --branch rc-cli-project-setup-workflows
  rc skills install --all
  rc skills install --agent codex --yes --no-input
  rc skills install --skill create-revenuecat-project --yes --no-input`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			if rt.Globals.NoInput && !rt.Globals.AssumeYes {
				return errors.New("installing skills under --no-input requires --yes")
			}
			branch = valueOrEnv(branch, "RC_SKILLS_BRANCH")
			source := officialToolkitSource
			if branch != "" {
				source = "https://github.com/RevenueCat/ai-toolkit/tree/" + branch
			}
			selectedSkills := append([]string(nil), skills...)
			if len(selectedSkills) == 0 && !all {
				selectedSkills = append(selectedSkills, defaultToolkitSkills...)
			}
			selectedAgents := append([]string(nil), agents...)
			if len(selectedAgents) == 0 {
				selectedAgents = append(selectedAgents, defaultToolkitAgents...)
			}
			args := []string{"--yes", "skills", "add", source}
			if !project {
				args = append(args, "--global")
			}
			args = append(args, "--agent")
			args = append(args, selectedAgents...)
			if len(selectedSkills) > 0 {
				args = append(args, "--skill")
				args = append(args, selectedSkills...)
			}
			if copyFiles {
				args = append(args, "--copy")
			}
			args = append(args, "--yes")
			if err := installer.Run(cmd, args); err != nil {
				return fmt.Errorf("install RevenueCat AI Toolkit: %w", err)
			}
			scope := "global"
			if project {
				scope = "project"
			}
			rt.Out.Success("Installed the RevenueCat AI Toolkit")
			rt.Out.Info("Start a new agent session or reload the agent to discover the installed skills.")
			rt.Out.Hint("Copy-ready starter prompts:  rc skills prompts")
			result := map[string]any{
				"installed":       true,
				"source":          source,
				"branch":          branch,
				"scope":           scope,
				"agents":          selectedAgents,
				"agent_selection": map[bool]string{true: "explicit", false: "revenuecat_defaults"}[len(agents) > 0],
				"skills":          selectedSkills,
				"all":             all,
				"command":         "npx " + strings.Join(args[1:], " "),
				"docs_url":        officialToolkitDocs,
				"next_step":       "Start a new agent session or reload the agent.",
				"trigger_example": projectSkillTrigger,
				"prompts_command": "rc skills prompts",
			}
			if rt.Out.IsJSON() {
				return rt.Out.Render(result)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "install globally (the default; retained for explicit scripts)")
	cmd.Flags().BoolVar(&project, "project", false, "install in the current project instead of globally")
	cmd.Flags().StringVar(&branch, "branch", "", "ai-toolkit branch to install (env: RC_SKILLS_BRANCH)")
	cmd.MarkFlagsMutuallyExclusive("global", "project")
	cmd.Flags().StringSliceVar(&agents, "agent", nil, "override target agents (default: RevenueCat-supported clients)")
	cmd.Flags().StringSliceVar(&skills, "skill", nil, "specific toolkit skills to install instead of the core setup bundle")
	cmd.Flags().BoolVar(&all, "all", false, "install all RevenueCat skills instead of the core setup bundle")
	cmd.MarkFlagsMutuallyExclusive("skill", "all")
	cmd.Flags().BoolVar(&copyFiles, "copy", false, "copy skill files instead of symlinking them")
	return cmd
}
