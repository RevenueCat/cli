package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
)

func newChartsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "charts",
		Aliases: []string{"chart"},
		Short:   "Inspect project charts",
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
		Use:   "list",
		Short: "List valid chart names",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			rows := make([][]string, len(api.ValidChartNames))
			for i, n := range api.ValidChartNames {
				rows[i] = []string{n}
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"NAME"},
				Rows:    rows,
				Raw:     map[string]any{"names": api.ValidChartNames},
			})
		},
	}
}

func newChartsShowCmd() *cobra.Command {
	var filterFlags []string
	cmd := &cobra.Command{
		Use:   "show <chart-name>",
		Short: "Show chart data for a named chart",
		Long: fmt.Sprintf(`Show data for a chart. Pass repeated --filter key=value flags to apply
chart-specific filters (see 'rc charts options <name>' for available filters).

Valid names: %s`, strings.Join(api.ValidChartNames, ", ")),
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
			data, err := client.Charts.Show(cmd.Context(), projectID, name, filters)
			if err != nil {
				return err
			}
			return rt.Out.Render(data)
		},
	}
	cmd.Flags().StringArrayVar(&filterFlags, "filter", nil, "filter key=value (repeatable)")
	return cmd
}

func newChartsOptionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "options <chart-name>",
		Short:             "Show available filters/segments for a chart",
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
