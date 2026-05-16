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
		Long: `Authenticate with RevenueCat by storing an API key in the active profile.

All inputs can be supplied via flags for non-interactive use:
  rc login --api-key sk_... --project-id proj_abc --no-input`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())

			// Every prompt is also a flag. tui.Form only renders fields that are unset.
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
