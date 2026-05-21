package cli

import (
	"fmt"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

func newProjectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "projects",
		Aliases: []string{"project"},
		Short:   "List and switch between projects",
		// Bare `rc projects` runs `list` for convenience.
		RunE: runProjectsList,
	}
	cmd.AddCommand(
		newProjectsListCmd(),
		newProjectsShowCmd(),
		newProjectsUseCmd(),
	)
	return cmd
}

func newProjectsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List projects",
		RunE:  runProjectsList,
	}
}

func runProjectsList(cmd *cobra.Command, _ []string) error {
	rt := RuntimeFrom(cmd.Context())
	client, err := rt.API()
	if err != nil {
		return err
	}
	page, err := client.Projects.List(cmd.Context())
	if err != nil {
		return err
	}

	if !rt.Globals.JSON && !rt.Globals.NoInput && tui.IsInteractive() {
		items := make([]tui.BrowserItem, len(page.Items))
		for i, p := range page.Items {
			p := p
			meta := formatMillis(p.CreatedAt)
			if p.ID == rt.Config.ProjectID {
				meta = "active · " + meta
			}
			items[i] = tui.BrowserItem{
				ID:    p.ID,
				Label: p.Name,
				Meta:  meta,
				Fields: []tui.BrowserField{
					{Key: "ID", Value: p.ID},
					{Key: "Name", Value: p.Name},
					{Key: "Created", Value: formatMillis(p.CreatedAt)},
				},
				DirectLoad: func() (string, []string, []tui.BrowserItem, error) {
					return p.Name, nil, browseHubItems(cmd.Context(), client, p.ID, rt.Globals.NoColor), nil
				},
			}
		}
		return tui.RunBrowser("Projects", items)
	}

	active := rt.Config.ProjectID
	rows := make([][]string, 0, len(page.Items))
	for _, p := range page.Items {
		marker := " "
		if p.ID == active {
			marker = "*"
		}
		rows = append(rows, []string{marker, p.ID, p.Name, formatMillis(p.CreatedAt)})
	}
	return rt.Out.RenderTable(output.Table{
		Columns: []string{"", "ID", "NAME", "CREATED"},
		Rows:    rows,
		Raw:     page,
	})
}

func newProjectsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [project-id]",
		Short: "Show a project's details",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			client, err := rt.API()
			if err != nil {
				return err
			}
			id := ""
			if len(args) == 1 {
				id = args[0]
			} else {
				id = rt.Config.ProjectID
			}
			if id == "" {
				return fmt.Errorf("no project ID given and no active project: pass an ID or run `rc projects use`")
			}
			p, err := client.Projects.Get(cmd.Context(), id)
			if err != nil {
				return err
			}
			if !rt.Globals.JSON && !rt.Globals.NoInput && tui.IsInteractive() {
				return tui.RunBrowser(p.Name, browseHubItems(cmd.Context(), client, p.ID, rt.Globals.NoColor))
			}
			return rt.Out.Render(p)
		},
	}
}

func newProjectsUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use [project-id]",
		Short: "Set the active project for this profile",
		Long: `Sets the default project for the current profile so subsequent commands
don't need --project-id.

Without an argument, opens an interactive picker. Under --no-input the
project ID is required.

The chosen project is written to the active profile file (default:
~/.config/revenuecat/default.json).`,
		Example: `  rc projects use                     # interactive picker
  rc projects use proj_abc            # explicit ID
  rc projects use proj_abc --json     # machine-readable confirmation`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			client, err := rt.API()
			if err != nil {
				return err
			}

			var chosen string
			if len(args) == 1 {
				chosen = args[0]
			} else {
				page, err := client.Projects.List(cmd.Context())
				if err != nil {
					return err
				}
				if len(page.Items) == 0 {
					return fmt.Errorf("no projects available on this account")
				}
				if len(page.Items) == 1 {
					chosen = page.Items[0].ID
					rt.Out.Info(fmt.Sprintf("Only one project available: %s (%s)", page.Items[0].Name, chosen))
				} else {
					const noDefault = "__no_default__"
					opts := make([]huh.Option[string], 0, len(page.Items)+1)
					opts = append(opts, huh.NewOption("Ask me every time  (don't save a default)", noDefault))
					for _, p := range page.Items {
						opts = append(opts, huh.NewOption(fmt.Sprintf("%s  (%s)", p.Name, p.ID), p.ID))
					}
					sel := huh.NewSelect[string]().
						Title("Project").
						Description("Type to filter  ·  Enter to confirm").
						Options(opts...).
						Filtering(true).
						Value(&chosen)
					if err := tui.Form(rt.Globals.NoInput).Field(sel).Run(); err != nil {
						return err
					}
					if chosen == noDefault {
						rt.Config.ProjectID = ""
						if err := config.Save(rt.Globals.Profile, rt.Config); err != nil {
							return err
						}
						rt.Out.Success("No default project set — you'll be prompted on each command.")
						return rt.Out.Render(map[string]any{
							"profile":    config.ProfileName(rt.Globals.Profile),
							"project_id": "",
						})
					}
				}
			}

			// Verify and resolve name.
			p, err := client.Projects.Get(cmd.Context(), chosen)
			if err != nil {
				return err
			}

			rt.Config.ProjectID = p.ID
			if err := config.Save(rt.Globals.Profile, rt.Config); err != nil {
				return err
			}

			rt.Out.Success(fmt.Sprintf("Active project: %s (%s)", p.Name, p.ID))
			return rt.Out.Render(map[string]any{
				"profile":    config.ProfileName(rt.Globals.Profile),
				"project_id": p.ID,
				"name":       p.Name,
			})
		},
	}
}

func formatMillis(m int64) string {
	if m == 0 {
		return ""
	}
	return time.UnixMilli(m).UTC().Format("2006-01-02")
}

// formatMillisPtr formats a nullable millisecond timestamp pointer.
func formatMillisPtr(m *int64) string {
	if m == nil {
		return ""
	}
	return formatMillis(*m)
}

// derefStr dereferences a *string safely, returning "" if nil.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// dashboardProjectID strips the alphabetic type prefix (and optional underscore
// separator) from an API resource ID so it can be used in dashboard URLs.
//
//	"proj5adb8697"  → "5adb8697"   (no underscore)
//	"app_abc123"    → "abc123"     (underscore separator)
//	"ofrng_xyz"     → "xyz"
func dashboardProjectID(id string) string {
	i := 0
	for i < len(id) && ((id[i] >= 'a' && id[i] <= 'z') || (id[i] >= 'A' && id[i] <= 'Z')) {
		i++
	}
	if i < len(id) && id[i] == '_' {
		i++
	}
	if i < len(id) {
		return id[i:]
	}
	return id
}
