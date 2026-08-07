package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

func newChartsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "charts",
		Aliases: []string{"chart"},
		Short:   "Explore subscription metric charts",
		Long: `Explore subscription metric charts — Active Subscriptions, MRR, Revenue, Churn,
Trial Conversion, and more — each sliceable with filters and segments. Run
without arguments to list the available charts, then 'show' one or inspect its
'options'.`,
		Example: `  rc charts list
  rc charts show mrr
  rc charts show actives --filter store=app_store`,
		RunE: runChartsList,
	}
	cmd.AddCommand(
		newChartsListCmd(),
		newChartsShowCmd(),
		newChartsOptionsCmd(),
	)
	return cmd
}

// `rc charts list` is intentionally a static command — there's no list
// endpoint, but the API enforces a closed enum of 22 chart names. Surfacing
// it locally helps both humans and LLM agents discover what's available
// without making a request that 400s.
func newChartsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List available chart names",
		Long:    `Lists the chart names accepted by 'rc charts show'. The set is a fixed enum, so this works without a request. In a terminal it opens the interactive chart browser.`,
		Example: "  rc charts list\n  rc charts list --json | jq -r '.names[]'",
		RunE:    runChartsList,
	}
}

func runChartsList(cmd *cobra.Command, _ []string) error {
	rt := RuntimeFrom(cmd.Context())

	// The interactive browser renders live chart data, which needs auth and a
	// project; when either is unavailable this stays a static command and
	// falls through to the plain list.
	if !rt.Globals.JSON && !rt.Globals.NoInput && tui.IsInteractive() {
		if projectID, err := requireProject(rt); err == nil {
			if client, err := rt.API(); err == nil {
				items := chartActionItems(cmd.Context(), client, projectID, rt.Globals.NoColor)
				return tui.RunBrowserTable("Charts", []string{"NAME"}, items)
			}
		}
		rt.Out.Info("Not signed in — showing the static chart list.")
		rt.Out.Hint("Log in for the interactive browser:  rc login")
	}

	rows := make([][]string, len(api.ValidChartNames))
	for i, n := range api.ValidChartNames {
		rows[i] = []string{n}
	}
	return rt.Out.RenderTable(output.Table{
		Columns: []string{"NAME"},
		Rows:    rows,
		Raw:     map[string]any{"names": api.ValidChartNames},
	})
}

func newChartsShowCmd() *cobra.Command {
	var filterFlags []string
	cmd := &cobra.Command{
		Use:   "show <chart-name>",
		Short: "Show data for a named chart",
		Long: fmt.Sprintf(`Show data for a named chart. The chart name is validated client-side
against a fixed enum (the API enforces the same enum server-side; we validate
locally to avoid a 400 round-trip and to provide shell completion).

Pass repeated --filter key=value flags to apply chart-specific filters.
Run 'rc charts options <name>' to see available filters and segments for
a specific chart.

Valid names:
  %s`, strings.Join(api.ValidChartNames, ", ")),
		Example: `  rc charts show mrr
  rc charts show mrr --json
  rc charts show actives --filter store=app_store --filter platform=ios
  rc charts options mrr   # discover what filters mrr accepts`,
		Args:              cobra.ExactArgs(1),
		ValidArgs:         api.ValidChartNames,
		ValidArgsFunction: cobra.FixedCompletions(api.ValidChartNames, cobra.ShellCompDirectiveNoFileComp),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			name := args[0]
			if !api.IsValidChartName(name) {
				return fmt.Errorf("invalid chart name %q — run `rc charts list` for valid names", name)
			}
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			filters := map[string]string{}
			for _, f := range filterFlags {
				k, v, ok := strings.Cut(f, "=")
				if !ok {
					return fmt.Errorf("--filter must be key=value, got %q", f)
				}
				filters[k] = v
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			data, err := client.Charts.Show(cmd.Context(), projectID, name, api.ChartShowOptions{Filters: filters})
			if err != nil {
				return err
			}
			if rt.Globals.JSON {
				return rt.Out.Render(data)
			}
			fetchFn := func(resID string, startUnix int64) (*api.ChartData, error) {
				return client.Charts.Show(context.Background(), projectID, name, api.ChartShowOptions{
					Resolution: resID,
					StartDate:  startUnix,
					Filters:    filters,
				})
			}
			return tui.RunChartView(data, fetchFn, rt.Globals.NoColor)
		},
	}
	cmd.Flags().StringArrayVar(&filterFlags, "filter", nil, "filter key=value (repeatable)")
	return cmd
}

func newChartsOptionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "options <chart-name>",
		Short:             "Show a chart's filters and segments",
		Long:              `Lists the filters and segments a chart accepts, so you know what to pass to 'rc charts show --filter'.`,
		Example:           "  rc charts options mrr\n  rc charts options actives --json",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: cobra.FixedCompletions(api.ValidChartNames, cobra.ShellCompDirectiveNoFileComp),
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
			opts, err := client.Charts.Options(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			return rt.Out.Render(opts)
		},
	}
}
