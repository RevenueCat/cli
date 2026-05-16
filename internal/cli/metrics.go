package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/output"
)

func newMetricsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "metrics",
		Short: "Show the project metrics overview",
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
				rows = append(rows, []string{m.ID, m.Name, fmt.Sprintf("%v", m.Value), m.Unit, m.Period})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "NAME", "VALUE", "UNIT", "PERIOD"},
				Rows:    rows,
				Raw:     overview,
			})
		},
	}
}

func newBenchmarksCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "benchmarks",
		Short: "Show project benchmarks",
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
			b, err := client.Benchmarks.Get(cmd.Context(), projectID)
			if err != nil {
				return err
			}
			return rt.Out.Render(b)
		},
	}
}
