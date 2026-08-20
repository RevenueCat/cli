package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

func newProjectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "projects",
		Aliases: []string{"project"},
		Short:   "List and switch between Projects",
		Long: `A Project groups your apps, catalog (Entitlements, Offerings, Packages,
Products), and settings. Most commands run against one active Project; set it
once with rc projects use so you don't pass --project-id every time.`,
		Example: `  rc projects list
  rc projects use proj_x`,
		// Bare `rc projects` runs `list` for convenience.
		RunE: runProjectsList,
	}
	cmd.AddCommand(
		newProjectsListCmd(),
		newProjectsShowCmd(),
		newProjectsCreateCmd(),
		newProjectsUseCmd(),
	)
	return cmd
}

func newProjectsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List projects",
		Long:  `Lists the Projects on your account. The active Project is marked with an asterisk.`,
		Example: `  rc projects list
  rc projects list --json | jq '.data.items[].id'`,
		RunE: runProjectsList,
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

	if rt.CanPrompt() {
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
		Use:     "show [project-id]",
		Short:   "Show a project's details",
		Long:    `Shows a Project's details. Defaults to the active Project when no ID is given.`,
		Example: "  rc projects show proj_x\n  rc projects show                 # active project",
		Args:    cobra.MaximumNArgs(1),
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
			if rt.CanPrompt() {
				return tui.RunBrowser(p.Name, browseHubItems(cmd.Context(), client, p.ID, rt.Globals.NoColor))
			}
			return rt.Out.Render(p)
		},
	}
}

func newProjectsCreateCmd() *cobra.Command {
	var name string
	var use bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a project",
		Long: `Creates a new RevenueCat project.

Use --use to save the newly-created project as the active project for the
current profile.`,
		Example: `  rc projects create --name "Acme App" --use
  rc projects create --name "Acme App" --json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			if err := tui.Form(rt.Globals.NoInput).
				Field(huh.NewInput().Title("Project name").Value(&name).Validate(tui.Required("name"))).
				Run(); err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			p, err := client.Projects.Create(cmd.Context(), api.ProjectCreate{Name: name})
			if err != nil {
				return err
			}
			if use {
				rt.Config.UseProjectID(p.ID)
				if err := config.Save(rt.Globals.Profile, rt.Config); err != nil {
					return err
				}
			}
			rt.Out.Success(fmt.Sprintf("Created project %s", p.ID))
			var shadow *dirBindingShadow
			if use {
				rt.Out.Success(fmt.Sprintf("Active project: %s (%s)", p.Name, p.ID))
				shadow = warnDirBindingShadow(rt, p.ID)
			}
			result := map[string]any{
				"project": p,
				"profile": map[string]any{
					"name":       config.ProfileName(rt.Globals.Profile),
					"project_id": rt.Config.ProjectID,
				},
			}
			if shadow != nil {
				result["dir_binding_shadow"] = shadow
			}
			return rt.Out.Render(result)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "project name (required)")
	cmd.Flags().BoolVar(&use, "use", false, "set the new project as active for this profile")
	return cmd
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
			} else if !rt.CanPrompt() {
				// No picker available: erroring beats the silent clear that the
				// non-interactive select otherwise resolves to.
				return fmt.Errorf("no project specified: pass a project ID (e.g. `rc projects use proj_abc`); the interactive picker is unavailable under --json/--no-input")
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
						result := map[string]any{
							"profile":    config.ProfileName(rt.Globals.Profile),
							"project_id": "",
						}
						if shadow := warnDirBindingShadow(rt, ""); shadow != nil {
							result["dir_binding_shadow"] = shadow
						}
						return rt.Out.Render(result)
					}
				}
			}

			// Verify and resolve name.
			p, err := client.Projects.Get(cmd.Context(), chosen)
			if err != nil {
				return err
			}

			rt.Config.UseProjectID(p.ID)
			if err := config.Save(rt.Globals.Profile, rt.Config); err != nil {
				return err
			}

			rt.Out.Success(fmt.Sprintf("Active project: %s (%s)", p.Name, p.ID))
			result := map[string]any{
				"profile":    config.ProfileName(rt.Globals.Profile),
				"project_id": p.ID,
				"name":       p.Name,
			}
			if shadow := warnDirBindingShadow(rt, p.ID); shadow != nil {
				result["dir_binding_shadow"] = shadow
			}
			return rt.Out.Render(result)
		},
	}
}

// dirBindingShadow describes a committed .revenuecat.json binding that
// outranks the profile default. It is surfaced in --json results so agents
// (which run in --json, where Warn/Hint are no-ops) still see the shadowing.
type dirBindingShadow struct {
	ProjectID string `json:"project_id"`
	File      string `json:"file"`
}

// warnDirBindingShadow reports a committed .revenuecat.json binding in the
// current tree that resolves to a different project than the profile default
// just set or cleared. The precedence (binding > profile default) is
// intentional; the hazard is that it's silent, so name the binding's project
// and file and state that commands here use it until the file changes. In
// human mode it emits a Warn+Hint; in every mode it returns the shadow (nil
// when nothing shadows) so callers can include it in --json output. selected
// is the id just persisted, or "" when the default was cleared (any binding
// then shadows it and keeps suppressing the prompt).
func warnDirBindingShadow(rt *Runtime, selected string) *dirBindingShadow {
	path, bound, ok := config.DirBinding()
	if !ok || bound == selected {
		return nil
	}
	rt.Out.Warn(fmt.Sprintf("A directory binding overrides this: %s pins project %s.", path, bound))
	rt.Out.Hint(fmt.Sprintf("Commands run in this directory will use %s, not the profile default, until you change or remove %s.", bound, path))
	return &dirBindingShadow{ProjectID: bound, File: path}
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

// dashboardProjectID strips the "proj" prefix (and an optional "_" separator) from a project ID for use in dashboard URLs.
func dashboardProjectID(id string) string {
	s := strings.TrimPrefix(id, "proj")
	if s == id {
		return id
	}
	return strings.TrimPrefix(s, "_")
}
