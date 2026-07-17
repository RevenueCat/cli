package cli

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/tui"
)

func newLoginCmd() *cobra.Command {
	var apiKey string
	var projectID string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with RevenueCat",
		Long: `Stores an API key (and optional default project) in the active profile.
The profile is written to ~/.config/revenuecat/<profile>.json with 0600
permissions.

All inputs can be supplied via flags for non-interactive use. The API key
can also be supplied via the RC_API_KEY environment variable instead of
storing it on disk — useful for CI.`,
		Example: `  # Interactive
  rc login

  # Non-interactive
  rc login --api-key sk_... --project-id proj_abc --no-input

  # Use a non-default profile (e.g. staging vs prod)
  rc login --profile staging --api-key sk_staging_...

  # CI: don't store on disk; pass via env each invocation
  RC_API_KEY=sk_... rc customer list`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())

			// Print the dashboard URL before prompting so users know where to
			// get a key. We deliberately don't auto-open a browser — works in
			// headless contexts and avoids the "did Claude just open my
			// browser?" surprise.
			if !rt.Globals.NoInput {
				rt.Out.Info("Generate an API key at https://app.revenuecat.com/settings/api-keys")
			}

			// Every prompt is also a flag. If a flag is set, huh will use it as the
			// field's default value (the user can still edit it in interactive mode,
			// or in --no-input mode it will be validated as-is).
			err := tui.Form(rt.Globals.NoInput).
				Field(huh.NewInput().
					Title("RevenueCat API key").
					EchoMode(huh.EchoModePassword).
					Value(&apiKey).
					Validate(tui.Required("API key"))).
				Field(huh.NewInput().
					Title("Default project ID (optional)").
					Value(&projectID)).
				Run()
			if err != nil {
				return err
			}

			rt.Config.APIKey = apiKey
			rt.Config.ProjectID = projectID
			if err := config.Save(rt.Globals.Profile, rt.Config); err != nil {
				return err
			}

			rt.Out.Success(fmt.Sprintf("Logged in (profile: %s)", config.ProfileName(rt.Globals.Profile)))
			return rt.Out.Render(map[string]any{
				"profile":    config.ProfileName(rt.Globals.Profile),
				"project_id": projectID,
			})
		},
	}

	cmd.Flags().StringVar(&apiKey, "api-key", "", "RevenueCat API key (or set RC_API_KEY)")
	cmd.Flags().StringVar(&projectID, "project-id", "", "default project ID")
	return cmd
}
