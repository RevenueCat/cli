package cli

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/tui"
)

func newBrowseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "browse",
		Short: "Browse the active project in an interactive hub",
		Long: `Opens a full-screen interactive hub for the active Project. Navigate Customers,
Entitlements, Offerings, Products, apps, webhooks, and charts, and drill into
records without leaving the terminal.

Requires an interactive terminal. Pass --json or --no-input to disable.`,
		Example: `  rc browse`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			if rt.Globals.JSON || rt.Globals.NoInput || !tui.IsInteractive() {
				return fmt.Errorf("rc browse requires an interactive terminal; use individual subcommands with --json or --no-input instead")
			}
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			items := browseHubItems(cmd.Context(), client, projectID, rt.Globals.NoColor)
			return tui.RunBrowser(fmt.Sprintf("Project  %s", dashboardProjectID(projectID)), items)
		},
	}
}

func browseHubItems(ctx context.Context, client *api.Client, projectID string, noColor bool) []tui.BrowserItem {
	return []tui.BrowserItem{
		{
			ID:    "customers",
			Label: "Customers",
			Meta:  "browse customer records",
			DirectLoad: func() (string, []string, []tui.BrowserItem, error) {
				page, err := client.Customers.List(ctx, projectID, nil)
				if err != nil {
					return "", nil, nil, err
				}
				cols := []string{"ID", "PLATFORM", "COUNTRY", "FIRST SEEN", "LAST SEEN"}
				return "Customers", cols, customersToItems(ctx, client, projectID, page.Items), nil
			},
		},
		{
			ID:    "entitlements",
			Label: "Entitlements",
			Meta:  "access-level definitions",
			DirectLoad: func() (string, []string, []tui.BrowserItem, error) {
				page, err := client.Entitlements.List(ctx, projectID)
				if err != nil {
					return "", nil, nil, err
				}
				cols := []string{"ID", "LOOKUP KEY", "DISPLAY NAME"}
				items := make([]tui.BrowserItem, len(page.Items))
				for i, e := range page.Items {
					e := e
					items[i] = entitlementToItem(ctx, client, projectID, e)
				}
				return "Entitlements", cols, items, nil
			},
		},
		{
			ID:    "offerings",
			Label: "Offerings",
			Meta:  "product offerings and packages",
			DirectLoad: func() (string, []string, []tui.BrowserItem, error) {
				page, err := client.Offerings.List(ctx, projectID)
				if err != nil {
					return "", nil, nil, err
				}
				cols := []string{"ID", "LOOKUP KEY", "DISPLAY NAME", "STATE"}
				items := make([]tui.BrowserItem, len(page.Items))
				for i, o := range page.Items {
					o := o
					items[i] = offeringToItem(ctx, client, projectID, o)
				}
				return "Offerings", cols, items, nil
			},
		},
		{
			ID:    "apps",
			Label: "Apps",
			Meta:  "platform app integrations",
			DirectLoad: func() (string, []string, []tui.BrowserItem, error) {
				page, err := client.Apps.List(ctx, projectID)
				if err != nil {
					return "", nil, nil, err
				}
				cols := []string{"ID", "NAME", "TYPE", "CREATED"}
				items := make([]tui.BrowserItem, len(page.Items))
				for i, a := range page.Items {
					items[i] = appToItem(projectID, a)
				}
				return "Apps", cols, items, nil
			},
		},
		{
			ID:    "products",
			Label: "Products",
			Meta:  "store products catalog",
			DirectLoad: func() (string, []string, []tui.BrowserItem, error) {
				page, err := client.Products.List(ctx, projectID, nil)
				if err != nil {
					return "", nil, nil, err
				}
				cols := []string{"ID", "DISPLAY NAME", "TYPE", "STORE ID", "STATE"}
				items := make([]tui.BrowserItem, len(page.Items))
				for i, p := range page.Items {
					items[i] = productToItem(p)
				}
				return "Products", cols, items, nil
			},
		},
		{
			ID:    "webhooks",
			Label: "Webhooks",
			Meta:  "event delivery integrations",
			DirectLoad: func() (string, []string, []tui.BrowserItem, error) {
				page, err := client.Webhooks.List(ctx, projectID)
				if err != nil {
					return "", nil, nil, err
				}
				cols := []string{"ID", "URL", "STATUS"}
				items := make([]tui.BrowserItem, len(page.Items))
				for i, w := range page.Items {
					items[i] = webhookToItem(projectID, w)
				}
				return "Webhooks", cols, items, nil
			},
		},
		{
			ID:    "charts",
			Label: "Charts",
			Meta:  "analytics charts",
			DirectLoad: func() (string, []string, []tui.BrowserItem, error) {
				items := chartActionItems(ctx, client, projectID, noColor)
				return "Charts", []string{"NAME"}, items, nil
			},
		},
	}
}

// chartActionItems builds chart browser items where Enter embeds the chart
// viewer directly inside the browser (no separate program).
func chartActionItems(ctx context.Context, client *api.Client, projectID string, noColor bool) []tui.BrowserItem {
	items := make([]tui.BrowserItem, len(api.ValidChartNames))
	for i, name := range api.ValidChartNames {
		name := name
		items[i] = tui.BrowserItem{
			ID:    name,
			Label: name,
			Row:   []string{name},
			Fields: []tui.BrowserField{
				{Key: "Chart", Value: name},
			},
			OpenChart: func() (tea.Model, error) {
				data, err := client.Charts.Show(ctx, projectID, name, api.ChartShowOptions{})
				if err != nil {
					return nil, err
				}
				fetchFn := func(resID string, startUnix int64) (*api.ChartData, error) {
					return client.Charts.Show(ctx, projectID, name, api.ChartShowOptions{
						Resolution: resID,
						StartDate:  startUnix,
					})
				}
				return tui.NewChartApp(data, fetchFn, noColor), nil
			},
		}
	}
	return items
}
