package cli

import (
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/config"
)

func newWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the active profile and project",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			return rt.Out.Render(map[string]any{
				"profile":        config.ProfileName(rt.Globals.Profile),
				"project_id":     rt.Config.ProjectID,
				"authenticated":  rt.Config.APIKey != "",
				"base_url":       rt.Config.BaseURL,
			})
		},
	}
}
