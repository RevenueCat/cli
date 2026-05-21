package cli

import (
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/output"
)

// Globals bound to root persistent flags. Every subcommand reads these via context.
type Globals struct {
	JSON      bool
	NoInput   bool
	Quiet     bool
	Verbose   bool
	Profile   string
	APIKey    string
	ProjectID string
	Format    string // jsonpath-style projection, applied to --json output
	NoColor   bool
	AssumeYes bool
}

func NewRootCmd(version string) *cobra.Command {
	g := &Globals{}

	root := &cobra.Command{
		Use:   "rc",
		Short: "RevenueCat command line interface",
		Long: `rc is the RevenueCat command line interface.

Designed for humans and AI agents alike: every interactive prompt is also
available as a flag or environment variable, and every command supports
machine-readable --json output with a stable schema. Errors emit the same
JSON envelope shape as the v2 API so the same parser handles both.

Agent-friendly entrypoints:
  rc commands --json     full command tree
  rc schema <cmd>        per-command flag/arg/example schema
  rc <cmd> --json        machine-readable output
  rc <cmd> --no-input    fail rather than prompt
  rc <cmd> --yes         skip confirmations`,
		Example: `  # Human use
  rc login
  rc customer show cus_abc

  # Scripted use
  rc customer list --json | jq '.data.items[].id'
  RC_API_KEY=sk_... rc entitlements list --json

  # Agent discovery
  rc commands --json
  rc schema customer grant`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(g.Profile)
			if err != nil {
				return err
			}
			if g.APIKey != "" {
				cfg.APIKey = g.APIKey
			}
			if g.ProjectID != "" {
				cfg.ProjectID = g.ProjectID
			}
			ctx := WithRuntime(cmd.Context(), &Runtime{
				Globals: g,
				Config:  cfg,
				Out:     output.NewRenderer(cmd.OutOrStdout(), cmd.ErrOrStderr(), g.JSON, g.NoColor, g.Format),
			})
			cmd.SetContext(ctx)
			return nil
		},
	}

	pf := root.PersistentFlags()
	pf.BoolVar(&g.JSON, "json", false, "emit machine-readable JSON output")
	pf.BoolVar(&g.NoInput, "no-input", false, "disable interactive prompts; fail if input is required")
	pf.BoolVarP(&g.Quiet, "quiet", "q", false, "suppress non-essential output")
	pf.BoolVarP(&g.Verbose, "verbose", "v", false, "enable verbose logging")
	pf.StringVar(&g.Profile, "profile", "", "configuration profile to use (default: active profile)")
	pf.StringVar(&g.APIKey, "api-key", "", "RevenueCat API key (overrides profile; or set RC_API_KEY)")
	pf.StringVar(&g.ProjectID, "project-id", "", "RevenueCat project ID (overrides profile; or set RC_PROJECT_ID)")
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

	root.AddCommand(
		newAuthCmd(),
		loginAlias,
		whoamiAlias,
		newProfilesCmd(),
		newProjectsCmd(),
		newCustomersCmd(),
		newEntitlementsCmd(),
		newOfferingsCmd(),
		newProductsCmd(),
		newSubscriptionsCmd(),
		newPurchasesCmd(),
		newInvoicesCmd(),
		newWebhooksCmd(),
		newPaywallsCmd(),
		newChartsCmd(),
		newMetricsCmd(),
		newAuditCmd(),
		newBenchmarksCmd(),
		newAppsCmd(),
		newPackagesCmd(),
		newSchemaCmd(root),
		newCommandsCmd(root),
		newVersionCmd(),
	)

	return root
}
