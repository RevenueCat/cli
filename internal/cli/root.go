package cli

import (
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/output"
)

// Globals bound to root persistent flags. Every subcommand reads these via context.
type Globals struct {
	JSON        bool
	NoInput     bool
	Quiet       bool
	Verbose     bool
	Profile     string
	APIKey      string
	Format      string // jsonpath-style projection, applied to --json output
	NoColor     bool
	AssumeYes   bool
}

func NewRootCmd(version string) *cobra.Command {
	g := &Globals{}

	root := &cobra.Command{
		Use:   "rc",
		Short: "RevenueCat command line interface",
		Long: `rc is the RevenueCat command line interface.

Designed for humans and AI agents alike: every interactive prompt is also
available as a flag or environment variable, and every command supports
machine-readable --json output with a stable schema.`,
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
	pf.StringVar(&g.Format, "format", "", "jsonpath projection applied to --json output (e.g. .data[].id)")
	pf.BoolVar(&g.NoColor, "no-color", false, "disable ANSI color (also honors NO_COLOR)")
	pf.BoolVarP(&g.AssumeYes, "yes", "y", false, "assume yes for confirmation prompts")

	root.AddCommand(
		newLoginCmd(),
		newWhoamiCmd(),
		newProjectsCmd(),
		newCustomersCmd(),
		newEntitlementsCmd(),
		newOfferingsCmd(),
		newProductsCmd(),
		newSchemaCmd(root),
		newCommandsCmd(root),
	)

	return root
}
