package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/output"
)

func newMetricsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "metrics",
		Short:   "Show the project Overview metrics",
		Long:    `Shows the project Overview: the headline metrics — Active Trials, Active Subscriptions, MRR, and Revenue — each with its current value and period. For time series with filters and segments, use 'rc charts'.`,
		Example: "  rc metrics\n  rc metrics --json | jq '.metrics[] | {id, value}'",
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
			overview, err := client.Metrics.Overview(cmd.Context(), projectID)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(overview.Metrics))
			for _, m := range overview.Metrics {
				rows = append(rows, []string{m.ID, m.Name, fmt.Sprintf("%v", m.Value), m.Unit, string(m.Period)})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "NAME", "VALUE", "UNIT", "PERIOD"},
				Rows:    rows,
				Raw:     overview,
			})
		},
	}
}
