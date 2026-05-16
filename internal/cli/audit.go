package cli

import (
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
)

func newAuditCmd() *cobra.Command {
	var limit int
	var cursor string
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "List recent audit log entries",
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
			page, err := client.Audit.List(cmd.Context(), projectID, &api.ListAuditOptions{
				Limit:         limit,
				StartingAfter: cursor,
			})
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(page.Items))
			for _, log := range page.Items {
				rows = append(rows, []string{
					formatMillis(log.OccurredAt),
					log.ActionType,
					log.ActorIdentifier,
					log.TargetType,
					log.TargetIdentifier,
				})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"OCCURRED", "ACTION", "ACTOR", "TARGET TYPE", "TARGET"},
				Rows:    rows,
				Raw:     page,
			})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 25, "max log entries to return")
	cmd.Flags().StringVar(&cursor, "cursor", "", "pagination cursor")
	return cmd
}
