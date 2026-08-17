package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/tui"
)

// agentClient is an AI agent CLI that rc setup can hand the onboarding to.
type agentClient struct {
	Name       string // display name
	Binary     string // executable looked up in PATH
	ToolkitKey string // skills-CLI agent identifier for the toolkit install
	LaunchArgs func(prompt, autonomy string) []string
}

// Autonomy levels for the launched agent. "auto" maps to each agent's own
// auto-approval mode; "trusted" pre-approves the tools the setup journey
// actually uses (rc, file edits, builds) so the human isn't asked to approve
// every step of a run they already consented to; "full" removes approvals
// entirely; "manual" is the agent's default.
const (
	autonomyAuto    = "auto"
	autonomyTrusted = "trusted"
	autonomyFull    = "full"
	autonomyManual  = "manual"
)

// trustedClaudeTools pre-approves the setup journey's real tool usage: every
// rc command, file edits, and the platform build/test commands the skills run.
var trustedClaudeTools = []string{
	"Bash(rc:*)", "Bash(npx:*)",
	"Bash(xcodebuild:*)", "Bash(xcrun:*)", "Bash(pod:*)", "Bash(swift:*)",
	"Bash(./gradlew:*)", "Bash(gradle:*)", "Bash(flutter:*)", "Bash(dart:*)",
	"Bash(npm:*)", "Bash(yarn:*)", "Bash(pnpm:*)",
	// dev-loop long tail observed in live runs: sim control, tool managers,
	// opening the simulator/dashboard, VCS reads, and `cd X && build`
	// compounds (every segment of a compound must match a rule).
	"Bash(mise:*)", "Bash(tuist:*)", "Bash(git:*)",
	"Bash(bundle:*)", "Bash(fastlane:*)", "Bash(make:*)", "Bash(cd:*)",
	"Bash(mkdir:*)", "Bash(cat:*)", "Bash(ls:*)", "Bash(grep:*)", "Bash(jq:*)",
}

var agentClients = []agentClient{
	{"Claude Code", "claude", "claude-code", func(p, autonomy string) []string {
		name := setupSessionName()
		switch autonomy {
		case autonomyAuto:
			return []string{"-n", name, "--permission-mode", "auto", p}
		case autonomyTrusted:
			// Prompt first — --allowedTools is variadic and would swallow a
			// trailing positional. Patterns must be SEPARATE args: a
			// comma-joined value registers as one bogus pattern that
			// matches nothing (live-debugged: nothing was pre-approved).
			args := []string{"-n", name, p, "--permission-mode", "acceptEdits", "--allowedTools"}
			return append(args, trustedClaudeTools...)
		case autonomyFull:
			return []string{"-n", name, "--dangerously-skip-permissions", p}
		default:
			return []string{"-n", name, p}
		}
	}},
	{"Codex", "codex", "codex", func(p, autonomy string) []string {
		switch autonomy {
		case autonomyAuto, autonomyTrusted:
			return []string{"-a", "on-request", "-s", "workspace-write", p}
		case autonomyFull:
			return []string{"--dangerously-bypass-approvals-and-sandbox", p}
		default:
			return []string{p}
		}
	}},
	{"Cursor", "cursor-agent", "cursor", func(p, autonomy string) []string {
		if autonomy == autonomyAuto || autonomy == autonomyTrusted || autonomy == autonomyFull {
			return []string{"--force", p}
		}
		return []string{p}
	}},
	{"Gemini CLI", "gemini", "gemini-cli", func(p, autonomy string) []string {
		switch autonomy {
		case autonomyAuto, autonomyTrusted:
			return []string{"--approval-mode", "auto_edit", "-i", p}
		case autonomyFull:
			return []string{"--yolo", "-i", p}
		default:
			return []string{"-i", p}
		}
	}},
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
			"requires_human_reason": "interactively it launches a local AI agent; run non-interactively (rc setup --json) to get the setup prompt to follow directly",
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSetup(cmd)
		},
	}
	return cmd
}

func runSetup(cmd *cobra.Command) error {
	rt := RuntimeFrom(cmd.Context())
	if !rt.CanPrompt() {
		return runSetupAgentPrompt(cmd, rt)
	}

	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	projectLabel, platform, projStatus, appDirs := detectAppProject(dir)
	agents := detectAgents()

	rt.Out.Title("RevenueCat setup  ·  " + filepath.Base(dir))
	rt.Out.Lead("An AI agent sets up RevenueCat for this app — you approve each step.")
	rt.Out.Field("Project", projectLabel)
	rt.Out.Field("Location", collapseHome(dir))
	if len(agents) == 0 {
		rt.Out.Field("Agents", "none found", "install Claude Code, Codex, Cursor, or Gemini CLI, or copy the prompt below")
	}

	justAuthed := false
	if rt.Config == nil || rt.Config.BearerToken() == "" {
		if err := setupAuthenticate(cmd, rt); err != nil {
			return err
		}
		justAuthed = true
	}

	newProjectPending := false
	if rt.Config != nil && rt.Config.BearerToken() != "" {
		var err error
		newProjectPending, err = confirmSetupAccount(cmd, rt, dir, justAuthed)
		if err != nil {
			return err
		}
	}

	rt.Out.Info("Checking where this project stands…")
	stage := detectSetupStage(cmd, rt, platform)
	rt.Out.Field("Stage", stage.Label)

	switch projStatus {
	case projectAmbiguous:
		rt.Out.Info("Several project markers here — the agent will figure out which app and platform to set up.")
	case projectNonMobile:
		rt.Out.Info("This doesn't look like a mobile app — the agent will confirm what needs RevenueCat and pick the platform.")
	case projectNone:
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

	// Apple runs before the handoff (the agent can't do 2FA); deferred until then.
	applePending := (platform == "ios" || platform == "cross") &&
		(stage.PromptID == "test-store-ready" || stage.PromptID == "connect-apple")

	rt.Out.Title("Step 1 · Choose your agent")
	choice, err := pickSetupAgent(agents)
	if err != nil {
		return err
	}
	if choice == nil {
		rt.Out.Answer("Agent", "none — copy the prompt")
		rt.Out.Blank()
		rt.Out.Info("Run rc skills install, then paste this into any agent:")
		rt.Out.Info(setupAgentPrompt(rt, stage, applePending, projStatus, appDirs))
		rt.Out.Hint("more starter prompts:  rc skills prompts")
		return nil
	}
	rt.Out.Answer("Agent", choice.Name)

	rt.Out.Title("Step 2 · Autonomy")
	autonomy := autonomyAuto
	if err := tui.Form(false).
		Field(huh.NewSelect[string]().
			Title("How much can "+choice.Name+" do without stopping to ask?").
			Description("You can interrupt anytime.").
			Options(
				huh.NewOption("Auto — use "+choice.Name+"'s built-in auto-approve mode", autonomyAuto),
				huh.NewOption("Run freely — no approval prompts", autonomyFull),
				huh.NewOption("Pre-approve rc, edits, and builds; ask for anything unusual", autonomyTrusted),
				huh.NewOption("Ask me before each step", autonomyManual),
			).
			Value(&autonomy)).
		Run(); err != nil {
		return err
	}
	rt.Out.Answer("Autonomy", autonomyLabels[autonomy])

	rt.Out.Title("Step 3 · Skills")
	skillsScope := "project"
	if err := tui.Form(false).
		Field(huh.NewSelect[string]().
			Title("Install the RevenueCat skills here or globally?").
			Description("Project keeps them with this repo; global shares them across every project on this machine.").
			Options(
				huh.NewOption("This project only", "project"),
				huh.NewOption("Globally (all projects on this machine)", "global"),
			).
			Value(&skillsScope)).
		Run(); err != nil {
		return err
	}
	rt.Out.Answer("Skills", skillsScopeLabels[skillsScope])

	// Apple needs a browser + 2FA; setup always defers it to the agent hand-back.
	appleDeferred := applePending

	prompt := setupAgentPrompt(rt, stage, appleDeferred, projStatus, appDirs)

	rt.Out.Plan([]string{
		"Install the RevenueCat AI Toolkit skills for " + choice.Name,
		"Configure the RevenueCat MCP for " + choice.Name,
		"Launch " + choice.Name + " to build your Test Store catalog, paywall, and SDK integration",
	})
	if appleDeferred {
		rt.Out.Info("Apple is deferred — the agent finishes everything that doesn't need it, then hands you the exact commands to connect App Store Connect when you're ready to ship.")
	}
	if err := confirmOrAbort(rt, "Launch "+choice.Name+" now?"); err != nil {
		return err
	}

	// deferred to here so canceling setup above doesn't wipe the active project
	if newProjectPending {
		if err := config.Save(rt.Globals.Profile, rt.Config); err != nil {
			rt.Out.Warn("Couldn't save the cleared project to your profile: " + err.Error())
		}
	}

	rt.Out.Info("Installing the RevenueCat AI Toolkit skills…")
	toolkitSource := officialToolkitSource
	if branch := os.Getenv("RC_SKILLS_BRANCH"); branch != "" {
		toolkitSource = "https://github.com/RevenueCat/ai-toolkit/tree/" + branch
		rt.Out.Info("Using toolkit branch " + branch + " (RC_SKILLS_BRANCH)")
	}
	installArgs := []string{"--yes", "skills", "add", toolkitSource, "--agent", choice.ToolkitKey}
	if skillsScope == "global" {
		installArgs = append(installArgs, "--global")
	}
	installArgs = append(installArgs, "--skill")
	installArgs = append(installArgs, defaultToolkitSkills...)
	// The skills CLI is a whole guided UI of its own; inside setup it runs
	// silently — our flow owns the questions, its output appears only on
	// failure.
	npxPath, err := exec.LookPath("npx")
	if err != nil {
		return fmt.Errorf("npx is required to install the RevenueCat AI Toolkit: %w", err)
	}
	install := exec.CommandContext(cmd.Context(), npxPath, installArgs...)
	if out, err := install.CombinedOutput(); err != nil {
		tail := string(out)
		if len(tail) > 1200 {
			tail = tail[len(tail)-1200:]
		}
		return fmt.Errorf("install skills: %w\n%s", err, strings.TrimSpace(tail))
	}
	rt.Out.Info("Skills installed")

	configureAgentMCP(cmd, rt, choice)

	rt.Out.Info("Launching " + choice.Name + "…")
	rt.Out.Blank()
	agent := exec.CommandContext(cmd.Context(), choice.Binary, choice.LaunchArgs(prompt, autonomy)...)
	agent.Stdin, agent.Stdout, agent.Stderr = os.Stdin, os.Stdout, os.Stderr
	return agent.Run()
}

var autonomyLabels = map[string]string{
	autonomyAuto:    "the agent's built-in auto-approve mode",
	autonomyTrusted: "pre-approve rc, edits, builds; ask for the rest",
	autonomyManual:  "ask before each step",
	autonomyFull:    "run freely (no approval prompts)",
}

var skillsScopeLabels = map[string]string{
	"project": "this project only",
	"global":  "global",
}

// confirmSetupAccount confirms the account and picks the project for this app,
// defaulting a new app to a fresh project rather than the active one.
func confirmSetupAccount(cmd *cobra.Command, rt *Runtime, dir string, justAuthed bool) (newProjectPending bool, err error) {
	const (
		optNewProject = iota
		optExistingProject
		optSwitchAccount
		optContinue
	)
	for {
		active := rt.Config != nil && rt.Config.ProjectID != ""
		title := "Create a new RevenueCat project for " + filepath.Base(dir) + "?"
		opts := []huh.Option[int]{
			huh.NewOption("Yes — new project", optNewProject),
			huh.NewOption("Use an existing project", optExistingProject),
		}
		choice := optNewProject
		if active {
			// New stays the default: the active project is profile-global, not this dir's
			title = "Set up " + filepath.Base(dir) + " — new project, or continue an existing one?"
			opts = []huh.Option[int]{
				huh.NewOption("New project for "+filepath.Base(dir), optNewProject),
				huh.NewOption("Continue with "+activeProjectLabel(cmd, rt), optContinue),
				huh.NewOption("Use a different project", optExistingProject),
			}
		}
		// Switching accounts only makes sense if we didn't just log them in.
		if !justAuthed {
			opts = append(opts, huh.NewOption("Switch account", optSwitchAccount))
		}
		if err := tui.Form(false).
			Field(huh.NewSelect[int]().Title(title).Options(opts...).Value(&choice)).
			Run(); err != nil {
			return false, err
		}

		switch choice {
		case optContinue:
			return false, nil
		case optNewProject:
			// persisted after launch is confirmed, not here
			rt.Config.ProjectID = ""
			return true, nil
		case optExistingProject:
			use, _, err := cmd.Root().Find([]string{"projects", "use"})
			if err != nil || use == nil {
				return false, fmt.Errorf("couldn't open the project picker — run `rc projects use`, then rerun setup")
			}
			use.SetContext(cmd.Context())
			if err := use.RunE(use, nil); err != nil {
				rt.Out.Warn("Project not changed: " + err.Error())
				continue
			}
			return false, nil
		case optSwitchAccount:
			rt.Config.AccountEmail = ""
			rt.Config.AccountName = ""
			if err := loginWithOAuth(cmd.Context(), rt); err != nil {
				return false, err
			}
		}
	}
}

// activeProjectLabel resolves the active project's name via the API, falling
// back to the ID.
func activeProjectLabel(cmd *cobra.Command, rt *Runtime) string {
	id := rt.Config.ProjectID
	client, err := rt.API()
	if err != nil {
		return id
	}
	if p, err := client.Projects.Get(cmd.Context(), id); err == nil && p.Name != "" {
		return p.Name + " (" + id + ")"
	}
	return id
}

// setupAgentPrompt is the starter prompt handed to the agent, with the
// Apple-deferred instruction appended when relevant.
// joinSubdirs renders relative app subdirectories as "./a, ./b" for agent notes.
func joinSubdirs(dirs []string) string {
	out := make([]string, len(dirs))
	for i, d := range dirs {
		out[i] = "./" + d
	}
	return strings.Join(out, ", ")
}

func setupAgentPrompt(rt *Runtime, stage setupStage, appleDeferred bool, status projectStatus, appDirs []string) string {
	prompt := starterPromptByID(stage.PromptID) + setupToolingNote(rt) + setupProjectNote(status, appDirs)
	if appleDeferred {
		prompt += "\n\nApple: I have deliberately deferred connecting my Apple account. Do NOT pause, wait, or poll for Apple credentials at any point — complete every stage that does not require Apple (Test Store catalog, paywall, SDK integration, build verification) and finish your run by listing the Apple steps as remaining work with the exact commands I should run later."
	}
	return prompt
}

func setupProjectNote(status projectStatus, appDirs []string) string {
	switch status {
	case projectClear:
		if len(appDirs) == 1 {
			return "\n\nProject detection: the app is in ./" + appDirs[0] + ", not the current directory — cd into it (or target that path) before making changes."
		}
		return ""
	case projectAmbiguous:
		if len(appDirs) > 0 {
			return "\n\nProject detection: several app projects live in subdirectories (" + joinSubdirs(appDirs) + "); the current directory has none. Identify the specific app and platform that needs RevenueCat before making changes — do not infer one."
		}
		return "\n\nProject detection: this directory has several project markers, so it may be a monorepo or nested checkout. Identify the specific app and platform that needs RevenueCat before making changes — do not infer one from the directory."
	case projectNonMobile:
		return "\n\nProject detection: this directory does not look like a mobile app (it resembles a web or backend project). Confirm with the user what should get RevenueCat and which platform, rather than assuming iOS."
	case projectNone:
		return "\n\nProject detection: no recognizable app project was found here. Confirm the target app and platform with the user before proceeding."
	default:
		return ""
	}
}

// runSetupAgentPrompt emits the stage-aware setup prompt for a non-interactive
// (agent) run instead of launching a nested agent.
func runSetupAgentPrompt(cmd *cobra.Command, rt *Runtime) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	_, platform, projStatus, appDirs := detectAppProject(dir)
	stage := detectSetupStage(cmd, rt, platform)
	authed := rt.Config != nil && rt.Config.BearerToken() != ""
	applePending := (platform == "ios" || platform == "cross") &&
		(stage.PromptID == "test-store-ready" || stage.PromptID == "connect-apple")

	prompt := setupAgentPrompt(rt, stage, applePending, projStatus, appDirs) + setupAuthHandbackNote(authed)

	if rt.Globals.JSON {
		return rt.Out.Render(map[string]any{
			"prompt":         prompt,
			"stage":          stage.PromptID,
			"authenticated":  authed,
			"apple_deferred": applePending,
			"platform":       platform,
		})
	}
	fmt.Fprintln(cmd.OutOrStdout(), prompt)
	return nil
}

// setupAuthHandbackNote appends auth guidance when logged out: headless signup
// is fine, but existing-account login is a human step to hand back.
func setupAuthHandbackNote(authed bool) string {
	if authed {
		return ""
	}
	savePassword := ""
	if runtime.GOOS == "darwin" {
		savePassword = " --save-password"
	}
	return "\n\nAuth: you are not logged in. If the user has no RevenueCat account and wants to sign up, you can help them do it from the terminal once they've agreed to the RevenueCat Terms of Service and Privacy Policy: `rc auth signup --email <user's email> --name \"<user's name>\" --generate-password" + savePassword + " --accept-terms --no-input --json`. The user is the one signing up and accepting the terms; the `--accept-terms` flag records the consent they gave you, so run it only after they say yes. If the user already has an account, STOP and ask them to run `rc auth login` (a browser sign-in you cannot perform), then continue."
}

// setupStage is where this project stands in the onboarding journey; it
// selects which starter prompt the launched agent receives, so re-running
// rc setup always hands over the NEXT stage rather than starting over.
type setupStage struct {
	Label    string // shown on the state block
	PromptID string // starter prompt handed to the agent
}

// platformFromLabel maps the directory detection label to a platform hint so
// an Android-only project is never told to connect an Apple account.
func platformFromLabel(label string) string {
	switch {
	case strings.Contains(label, "Xcode"), strings.Contains(label, "Swift"), strings.Contains(label, "Tuist"):
		return "ios"
	case strings.Contains(label, "Android"):
		return "android"
	case strings.Contains(label, "Flutter"), strings.Contains(label, "React Native"), strings.Contains(label, "Expo"):
		return "cross"
	default:
		return "unknown"
	}
}

// detectSetupStage reads project state through the public API. Detection is
// deliberately local and cheap (three list calls); on any read error it
// falls back to the beginning, which is always safe because the skills
// themselves re-verify state.
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
	// directory is clearly iOS/cross-platform. An empty platform (nested,
	// non-mobile, or undetermined) defers to the agent rather than assuming Apple.
	appleRelevant := appStore != nil || (playStore == nil && (platform == "ios" || platform == "cross"))
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
	)
	choice := optLogin
	if err := tui.Form(false).
		Field(huh.NewSelect[int]().
			Title("You're not logged in — how would you like to sign in?").
			Options(
				huh.NewOption("Log in (opens your browser)", optLogin),
				huh.NewOption("Create a RevenueCat account", optSignup),
			).
			Value(&choice)).
		Run(); err != nil {
		return err
	}
	if choice == optSignup {
		signup := newAuthSignupCmd()
		signup.SetContext(cmd.Context())
		return signup.RunE(signup, nil)
	}
	return loginWithOAuth(cmd.Context(), rt)
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

type projectStatus int

const (
	projectClear projectStatus = iota
	projectAmbiguous
	projectNonMobile
	projectNone
)

type projectMarker struct {
	label    string
	platform string
}

func detectProjectMarkers(dir string) []projectMarker {
	var markers []projectMarker
	add := func(label string) {
		markers = append(markers, projectMarker{label, platformFromLabel(label)})
	}

	if matches, _ := filepath.Glob(filepath.Join(dir, "*.xcodeproj")); len(matches) > 0 {
		add("Xcode project (" + filepath.Base(matches[0]) + ")")
	}
	if matches, _ := filepath.Glob(filepath.Join(dir, "*.xcworkspace")); len(matches) > 0 {
		add("Xcode workspace (" + filepath.Base(matches[0]) + ")")
	}
	if _, err := os.Stat(filepath.Join(dir, "pubspec.yaml")); err == nil {
		add("Flutter app")
	}
	if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		switch {
		case strings.Contains(string(data), `"react-native"`):
			add("React Native app")
		case strings.Contains(string(data), `"expo"`):
			add("Expo app")
		default:
			add("JavaScript project")
		}
	}
	for _, tuist := range []string{"Project.swift", "Workspace.swift"} {
		if _, err := os.Stat(filepath.Join(dir, tuist)); err == nil {
			add("Tuist project (iOS)")
			break
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "Package.swift")); err == nil {
		add("Swift package")
	}
	for _, gradle := range []string{"settings.gradle", "settings.gradle.kts", "build.gradle", "build.gradle.kts"} {
		if _, err := os.Stat(filepath.Join(dir, gradle)); err == nil {
			add("Android project")
			break
		}
	}
	return markers
}

// classifyDir inspects one directory's markers. hasMarkers is false when it has
// none, so the caller can decide whether to look elsewhere (e.g. one level down).
func classifyDir(dir string) (label, platform string, status projectStatus, hasMarkers bool) {
	markers := detectProjectMarkers(dir)
	if len(markers) == 0 {
		return "", "", projectNone, false
	}

	// A non-mobile marker (a package.json for tooling like Fastlane or Prettier)
	// sits in many mobile repos, so it doesn't make the target ambiguous — only
	// distinct mobile platforms do.
	var mobile []projectMarker
	buckets := map[string]bool{}
	for _, m := range markers {
		if m.platform == "unknown" {
			continue
		}
		mobile = append(mobile, m)
		buckets[m.platform] = true
	}

	switch {
	case len(buckets) > 1:
		labels := make([]string, len(mobile))
		for i, m := range mobile {
			labels[i] = m.label
		}
		return "multiple projects detected (" + strings.Join(labels, ", ") + ")", "", projectAmbiguous, true
	case len(buckets) == 1:
		return mobile[0].label, mobile[0].platform, projectClear, true
	default:
		return markers[0].label + " (not a mobile app)", "", projectNonMobile, true
	}
}

// detectAppProject classifies the working directory, returning an empty platform
// for every non-clear case so applePending never fires and the flow defers to
// the agent. When the directory has no markers of its own, it looks one level
// down for apps in subdirectories (a common layout: the app lives in ./ios,
// ./app, or ./apps/mobile). appDirs holds those relative subdirectories — one
// for a single clear app, several for an ambiguous set — and is empty when the
// app (or the ambiguity) is the working directory itself.
func detectAppProject(dir string) (label, platform string, status projectStatus, appDirs []string) {
	if home, err := os.UserHomeDir(); err == nil && dir == home {
		return "this is your home directory, not an app", "", projectNone, nil
	}
	if l, p, s, ok := classifyDir(dir); ok {
		return l, p, s, nil
	}

	var subs []struct{ label, platform, rel string }
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() || skipSubdir(e.Name()) {
			continue
		}
		// Only a single clear mobile app in a subdir counts; a web/tooling subdir
		// or a subdir that is itself ambiguous doesn't compete.
		if l, p, s, ok := classifyDir(filepath.Join(dir, e.Name())); ok && s == projectClear {
			subs = append(subs, struct{ label, platform, rel string }{l, p, e.Name()})
		}
	}

	switch len(subs) {
	case 1:
		return subs[0].label + " (in ./" + subs[0].rel + ")", subs[0].platform, projectClear, []string{subs[0].rel}
	case 0:
		return "no app project detected", "", projectNone, nil
	default:
		labels := make([]string, len(subs))
		rels := make([]string, len(subs))
		for i, s := range subs {
			labels[i] = s.label + " (./" + s.rel + ")"
			rels[i] = s.rel
		}
		return "multiple app projects in subdirectories (" + strings.Join(labels, ", ") + ")", "", projectAmbiguous, rels
	}
}

// skipSubdir skips directories that never hold an app root but are expensive or
// misleading to scan: VCS metadata, dependencies, and build output.
func skipSubdir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "Pods", "Carthage", "build", "Build", "DerivedData", "vendor", "dist", "out", "target":
		return true
	}
	return false
}

// setupSessionName names the launched agent's session after the app directory.
func setupSessionName() string {
	dir, err := os.Getwd()
	if err != nil {
		return "RevenueCat setup"
	}
	return "RevenueCat setup (" + filepath.Base(dir) + ")"
}

func collapseHome(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(dir, home) {
		return "~" + strings.TrimPrefix(dir, home)
	}
	return dir
}
