package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/output"
)

// Globals bound to root persistent flags. Every subcommand reads these via context.
type Globals struct {
	JSON      bool
	NoInput   bool
	Quiet     bool
	Verbose   bool
	ShowAll   bool
	Profile   string
	APIKey    string
	ProjectID string
	Format    string // jq/gojq expression, applied to --json output
	NoColor   bool
	AssumeYes bool
	Version   string
}

func NewRootCmd(version string) *cobra.Command {
	g := &Globals{Version: version}

	root := &cobra.Command{
		Use:   "rc",
		Short: "RevenueCat command line interface",
		Long: `rc is the RevenueCat command line interface.

Getting started:
  rc setup               set up RevenueCat for the app in this directory
  rc auth login          log in (browser or an API key)
  rc projects use        choose a default project
  rc <command> --help    explore any command group

Designed for humans and AI agents alike: every interactive prompt is also
available as a flag or environment variable, and every command supports
machine-readable --json output with a stable schema. Errors emit the same
JSON envelope shape as the v2 API so the same parser handles both.

Agent-friendly entrypoints:
  rc commands --json     full command tree
  rc schema <cmd>        per-command flag/arg/example schema
  rc skills install      official RevenueCat AI Toolkit workflows
  rc <cmd> --json        machine-readable output
  rc <cmd> --no-input    fail rather than prompt
  rc <cmd> --yes         skip confirmations`,
		Example: `  # Human use
  rc auth login
  rc customers show cus_abc

  # Scripted use
  rc customers list --json | jq '.data.items[].id'
  RC_API_KEY=sk_... rc entitlements list --json

  # Agent discovery
  rc commands --json
  rc schema customers grant`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(g.Profile)
			if err != nil {
				return err
			}
			cfg.SetFlagAPIKey(g.APIKey)
			if g.ProjectID != "" {
				cfg.ProjectID = g.ProjectID
			}
			rt := &Runtime{
				Globals: g,
				Config:  cfg,
				Ctx:     cmd.Context(),
				Out:     output.NewRenderer(cmd.OutOrStdout(), cmd.ErrOrStderr(), g.JSON, g.NoColor, g.Quiet, g.Format),
			}
			cmd.SetContext(WithRuntime(cmd.Context(), rt))
			return nil
		},
	}

	pf := root.PersistentFlags()
	pf.BoolVar(&g.JSON, "json", false, "emit machine-readable JSON output")
	pf.BoolVar(&g.NoInput, "no-input", false, "disable interactive prompts; fail if input is required")
	pf.BoolVarP(&g.Quiet, "quiet", "q", false, "suppress non-essential output")
	pf.BoolVar(&g.ShowAll, "all", false, "also show experimental commands in --help")
	pf.BoolVarP(&g.Verbose, "verbose", "v", false, "enable verbose logging")
	_ = pf.MarkHidden("verbose")
	pf.StringVar(&g.Profile, "profile", "", "configuration profile to use (default: active profile)")
	pf.StringVar(&g.APIKey, "api-key", "", "RevenueCat API key (overrides profile; or set RC_API_KEY)")
	pf.StringVar(&g.ProjectID, "project-id", "", "RevenueCat project ID (highest precedence, then RC_PROJECT_ID, then .revenuecat.json in the directory tree, then the profile default)")
	pf.StringVar(&g.Format, "format", "", "jq expression applied to --json output (e.g. '.data.items[].id')")
	pf.BoolVar(&g.NoColor, "no-color", false, "disable ANSI color (also honors NO_COLOR)")
	pf.BoolVarP(&g.AssumeYes, "yes", "y", false, "assume yes for confirmation prompts")

	// Hidden top-level aliases for muscle memory / back-compat.
	loginAlias := newAuthLoginCmd()
	loginAlias.Use = "login"
	loginAlias.Hidden = true

	whoamiAlias := newAuthStatusCmd()
	whoamiAlias.Use = "whoami"
	whoamiAlias.Hidden = true

	// bare rc → state-aware getting-started; rc --help lists every command.
	// --all falls through to help instead of the home screen
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		if RuntimeFrom(cmd.Context()).Globals.ShowAll {
			return cmd.Help()
		}
		rt := RuntimeFrom(cmd.Context())
		writeHomeScreen(cmd.OutOrStdout(), rt)
		maybeNudgeSkillsInstall(rt)
		return nil
	}

	// Each command declares its help group here; GroupID lives on the command
	// (not a separate lookup), so adding a command means grouping it in one place.
	// Group IDs must exist in commandGroups (enforced by TestEveryCommandHasARegisteredGroup).
	grouped := []struct {
		cmd   *cobra.Command
		group string
	}{
		{newSetupCmd(), "start"},
		{newCapitalCmd(), "integrations"},
		{newOpenCmd(), "start"},
		{newAuthCmd(), "start"},
		{loginAlias, "start"},
		{whoamiAlias, "start"},
		{newProfilesCmd(), "start"},
		{newProjectsCmd(), "start"},
		{newBrowseCmd(), "start"},
		{newCustomersCmd(), "revenue"},
		{newEntitlementsCmd(), "catalog"},
		{newOfferingsCmd(), "catalog"},
		{newProductsCmd(), "catalog"},
		{newSubscriptionsCmd(), "revenue"},
		{newPurchasesCmd(), "revenue"},
		{newInvoicesCmd(), "revenue"},
		{newWebhooksCmd(), "integrations"},
		{newPaywallsCmd(), "design"},
		{newMediaAssetsCmd(), "design"},
		{newFontsCmd(), "design"},
		{newRicoCmd(), "ai"},
		{newChartsCmd(), "revenue"},
		{newMetricsCmd(), "revenue"},
		{newAuditCmd(), "advanced"},
		{newAppsCmd(), "integrations"},
		{newPackagesCmd(), "catalog"},
		{newAPICmd(), "advanced"},
		{newSkillsCmd(), "ai"},
		{newSchemaCmd(root), "advanced"},
		{newCommandsCmd(root), "advanced"},
		{newVersionCmd(), "advanced"},
	}
	for _, g := range grouped {
		g.cmd.GroupID = g.group
		root.AddCommand(g.cmd)
	}

	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return usageError{suggestFlag(cmd, err)}
	})
	applyCommandGroups(root)
	guardUnknownSubcommands(root)
	applySurfaceProfile(root)
	applyHelpStyling(root)

	// --help skips PersistentPreRunE, so re-apply the surface from the parsed
	// --all flag right before help renders, and footer the hidden count so a
	// human (or a skill-less agent) knows there's more.
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(c *cobra.Command, args []string) {
		helpColor = !g.NoColor && os.Getenv("NO_COLOR") == ""
		applySurfaceProfile(root)
		defaultHelp(c, args)
		if c == root && !showAllSurface(root) && !testing.Testing() {
			fmt.Fprintln(c.OutOrStdout(), "\nRun `rc --all` to include experimental commands, or `rc commands --schemas` for the full machine-readable surface.")
		}
	})
	return root
}

// guardUnknownSubcommands walks the whole tree so every group — top-level and
// nested — rejects an unknown subcommand instead of cobra's default of printing
// help and exiting 0, which reads as success to scripts and agents. (cobra only
// does that for the root.) A non-runnable group short-circuits to help before
// arg validation runs, so it also gets a help-only RunE to reach the check. A
// bare group (no args) still falls through to help or the group's own RunE, and
// groups that set their own Args (e.g. rico takes a message) are left alone.
func guardUnknownSubcommands(cmd *cobra.Command) {
	for _, sub := range cmd.Commands() {
		guardUnknownSubcommands(sub)
	}
	if !cmd.HasSubCommands() {
		return
	}
	if cmd.SuggestionsMinimumDistance <= 0 {
		cmd.SuggestionsMinimumDistance = 2 // SuggestionsFor reads this directly; cobra only defaults it inside its own help path.
	}
	if cmd.Args == nil {
		cmd.Args = func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			e := &unknownSubcommandError{parent: c.CommandPath(), name: args[0]}
			if s := c.SuggestionsFor(args[0]); len(s) > 0 {
				e.suggestion = s[0]
			}
			return e
		}
	}
	if !cmd.Runnable() {
		cmd.RunE = func(c *cobra.Command, _ []string) error { return c.Help() }
		if cmd.Annotations == nil {
			cmd.Annotations = map[string]string{}
		}
		cmd.Annotations["help_only"] = "true"
	}
}

// suggestFlag appends a did-you-mean to unknown-flag errors: agents guess
// short forms (--project for --project-id) and cobra offers command
// suggestions but not flag ones.
func suggestFlag(cmd *cobra.Command, err error) error {
	msg := err.Error()
	const prefix = "unknown flag: --"
	idx := strings.Index(msg, prefix)
	if idx < 0 {
		return err
	}
	unknown := strings.TrimSpace(msg[idx+len(prefix):])
	best, bestScore := "", 0
	seen := func(f *pflag.Flag) {
		score := 0
		if strings.HasPrefix(f.Name, unknown) || strings.HasPrefix(unknown, f.Name) {
			score = len(unknown)
			if len(f.Name) < len(unknown) {
				score = len(f.Name)
			}
		}
		if score > bestScore {
			best, bestScore = f.Name, score
		}
	}
	cmd.Flags().VisitAll(seen)
	cmd.InheritedFlags().VisitAll(seen)
	if best == "" || bestScore < 3 {
		return err
	}
	return fmt.Errorf("%s (did you mean --%s?)", msg, best)
}

// guidedMode is set by the npm launcher for npx runs (RC_GUIDED).
func guidedMode() bool {
	return os.Getenv("RC_GUIDED") != ""
}
