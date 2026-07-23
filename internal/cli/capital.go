package cli

import (
	"github.com/spf13/cobra"
)

// newCapitalCmd is the RevenueCat Capital entry point. Capital underwrites on
// App Store sales data, so its onboarding IS the App Store Connect key flow:
// `rc capital setup` reuses the guided Apple credential setup (sign-in, 2FA,
// keys, vendor number) under a name tied to the product. One of the two
// scoped npx surfaces.
func newCapitalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "capital",
		Short: "Set up RevenueCat Capital (connect App Store Connect)",
		Long: `RevenueCat Capital underwrites on your App Store sales data, so setup
connects your App Store Connect account: it signs in to Apple, creates the
App Store Connect API key and in-app purchase key, and captures your vendor
number so RevenueCat can read sales reports.

Your Apple credentials go from your machine directly to Apple and are never
saved or sent to RevenueCat.`,
		Example: `  rc capital setup
  rc capital setup appl_1a2b3c`,
	}
	// Reuse the Apple credential workflow verbatim: the "setup" subcommand is
	// the same guided flow shipped as `rc apps apple setup`.
	setup := newAppsAppleWorkflowCmd(false, newAppleConnectClient)
	setup.Short = "Connect App Store Connect for Capital"
	cmd.AddCommand(setup)
	return cmd
}
