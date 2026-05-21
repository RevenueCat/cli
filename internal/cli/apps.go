package cli

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

// App types verified against fixtures. New types should be added to the
// select picker as the platform grows.
var appTypes = []huh.Option[string]{
	huh.NewOption("App Store", "app_store"),
	huh.NewOption("Play Store", "play_store"),
	huh.NewOption("Amazon", "amazon"),
	huh.NewOption("Mac App Store", "mac_app_store"),
	huh.NewOption("Roku", "roku"),
	huh.NewOption("Stripe", "stripe"),
	huh.NewOption("Web Billing (RC Billing)", "rc_billing"),
}

func newAppsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "apps",
		Aliases: []string{"app"},
		Short:   "Manage apps in a project",
	}
	cmd.AddCommand(
		newAppsListCmd(),
		newAppsShowCmd(),
		newAppsCreateCmd(),
		newAppsUpdateCmd(),
		newAppsDeleteCmd(),
		newAppsKeysCmd(),
	)
	return cmd
}

func newAppsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List apps",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			page, err := client.Apps.List(cmd.Context(), projectID)
			if err != nil {
				return err
			}

			if !rt.Globals.JSON && !rt.Globals.NoInput && tui.IsInteractive() {
				items := make([]tui.BrowserItem, len(page.Items))
				for i, a := range page.Items {
					items[i] = tui.BrowserItem{
						ID:     a.ID,
						Label:  a.Name,
						Meta:   a.Type,
						WebURL: fmt.Sprintf("https://app.revenuecat.com/projects/%s/apps/%s", projectID, a.ID),
						Fields: []tui.BrowserField{
							{Key: "ID", Value: a.ID},
							{Key: "Name", Value: a.Name},
							{Key: "Type", Value: a.Type},
							{Key: "Created", Value: formatMillis(a.CreatedAt)},
						},
					}
				}
				return tui.RunBrowser("Apps", items)
			}

			rows := make([][]string, 0, len(page.Items))
			for _, a := range page.Items {
				rows = append(rows, []string{a.ID, a.Name, a.Type, formatMillis(a.CreatedAt)})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "NAME", "TYPE", "CREATED"},
				Rows:    rows,
				Raw:     page,
			})
		},
	}
}

func newAppsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show an app",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			a, err := client.Apps.Get(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			return rt.Out.Render(a)
		},
	}
}

func newAppsCreateCmd() *cobra.Command {
	var name, appType string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an app",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if err := tui.Form(rt.Globals.NoInput).
				Field(huh.NewInput().Title("App name").Value(&name).Validate(tui.Required("name"))).
				Field(huh.NewSelect[string]().Title("Type").Options(appTypes...).Value(&appType)).
				Run(); err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			a, err := client.Apps.Create(cmd.Context(), projectID, api.AppCreate{Name: name, Type: appType})
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Created app %s", a.ID))
			return rt.Out.Render(a)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "app name (required)")
	cmd.Flags().StringVar(&appType, "type", "", "app type: app_store|play_store|amazon|mac_app_store|roku|stripe|rc_billing")
	return cmd
}

func newAppsUpdateCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update an app",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			body := api.AppUpdate{}
			if cmd.Flags().Changed("name") {
				body.Name = &name
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			a, err := client.Apps.Update(cmd.Context(), projectID, args[0], body)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Updated %s", a.ID))
			return rt.Out.Render(a)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "new name")
	return cmd
}

func newAppsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete an app",
		Long: `Permanently deletes an app from the project. Disconnects RevenueCat
from the underlying store integration; existing customer data is retained
but no longer associated with this app.

Reversibility: irreversible.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc apps delete app_old --yes`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if !rt.Globals.AssumeYes {
				ok, err := tui.Confirm(rt.Globals.NoInput, fmt.Sprintf("Delete app %q?", args[0]))
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("aborted")
				}
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			if err := client.Apps.Delete(cmd.Context(), projectID, args[0]); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Deleted %s", args[0]))
			return rt.Out.Render(map[string]any{"ok": true, "id": args[0]})
		},
	}
}

func newAppsKeysCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "keys <app-id>",
		Short: "List public API keys for an app",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			keys, err := client.Apps.PublicAPIKeys(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			return rt.Out.Render(keys)
		},
	}
}
