package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/output"
)

// rc profiles — manage local credential profiles. Distinct from `rc projects`,
// which switches between RevenueCat projects within one profile.

func newProfilesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "profiles",
		Aliases: []string{"profile"},
		Short:   "List and switch between local credential profiles",
		Long: `A profile bundles an API key + default project + base URL for one
RevenueCat workspace. Multiple profiles let you keep e.g. staging and prod
separate. The active profile is resolved with precedence: --profile flag >
RC_PROFILE env > .active pointer file > "default".`,
		Example: `  rc profiles list
  rc profiles use staging
  rc profiles show prod
  rc profiles delete old`,
	}
	cmd.AddCommand(
		newProfilesListCmd(),
		newProfilesUseCmd(),
		newProfilesShowCmd(),
		newProfilesDeleteCmd(),
	)
	return cmd
}

func newProfilesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List configured profiles",
		Long:    `Lists the local credential profiles on disk, marking the active one and showing each profile's default project and whether it holds an API key.`,
		Example: `  rc profiles list`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			names, err := config.ListProfiles()
			if err != nil {
				return err
			}
			active := config.ProfileName("")
			rows := make([][]string, 0, len(names))
			for _, n := range names {
				marker := " "
				if n == active {
					marker = "*"
				}
				cfg, _ := config.LoadStored(n)
				project := ""
				if cfg != nil {
					project = cfg.ProjectID
				}
				authed := "no"
				if cfg != nil && cfg.APIKey != "" {
					authed = "yes"
				}
				rows = append(rows, []string{marker, n, project, authed})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"", "NAME", "PROJECT", "AUTHED"},
				Rows:    rows,
				Raw: map[string]any{
					"profiles": names,
					"active":   active,
				},
			})
		},
	}
}

func newProfilesUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Set the active profile",
		Long: `Writes a pointer file so the named profile is used by future commands
without needing --profile or RC_PROFILE.

The profile must already exist on disk (create it with ` + "`rc login --profile <name>`" + `).`,
		Example: `  rc profiles use staging`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			if err := config.SetActiveProfile(args[0]); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Active profile: %s", args[0]))
			return rt.Out.Render(map[string]any{"active": args[0]})
		},
	}
}

func newProfilesShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "show [name]",
		Short:   "Show a profile's resolved configuration (API key redacted)",
		Long:    `Shows the resolved project, base URL, and redacted API key for a profile. Defaults to the active profile when no name is given.`,
		Example: "  rc profiles show prod\n  rc profiles show                 # active profile",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			cfg, err := config.Load(name)
			if err != nil {
				return err
			}
			redacted := ""
			if cfg.APIKey != "" {
				redacted = "***" + lastN(cfg.APIKey, 4)
			}
			return rt.Out.Render(map[string]any{
				"profile":    config.ProfileName(name),
				"project_id": cfg.ProjectID,
				"base_url":   cfg.BaseURL,
				"api_key":    redacted,
			})
		},
	}
}

func lastN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func newProfilesDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a profile",
		Long: `Removes the profile's config file from disk. If the deleted profile was
active, the .active pointer is cleared and subsequent commands fall back to
"default".

Reversibility: irreversible. Re-create with ` + "`rc login --profile <name>`" + `.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc profiles delete old --yes`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			if err := confirmOrAbort(rt, fmt.Sprintf("Delete profile %q?", args[0])); err != nil {
				return err
			}
			if err := config.DeleteProfile(args[0]); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Deleted profile %q", args[0]))
			return rt.Out.Render(map[string]any{"ok": true, "name": args[0]})
		},
	}
}
