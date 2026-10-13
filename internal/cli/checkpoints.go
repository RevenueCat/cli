package cli

import (
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
)

func newCheckpointsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "checkpoints",
		Aliases: []string{"checkpoint"},
		Short:   "View Checkpoints",
		Long: `A Checkpoint is a point in your app, such as app open or after onboarding,
where Checkpoint rules decide which Flow a customer sees.`,
		Example: `  rc checkpoints list`,
	}
	cmd.AddCommand(newCheckpointsListCmd())
	return cmd
}

func newCheckpointsListCmd() *cobra.Command {
	var opts api.ListOptions
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Checkpoints",
		Long:  `Lists the project's Checkpoints with the identifier your app uses for each one.`,
		Example: `  rc checkpoints list
  rc checkpoints list --json | jq '.data.items[] | {id, identifier}'`,
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
			page, err := client.Checkpoints.List(cmd.Context(), projectID, &opts)
			if err != nil {
				return err
			}

			rows := make([][]string, 0, len(page.Items))
			for _, c := range page.Items {
				rows = append(rows, []string{c.ID, c.Identifier, nonEmpty(c.CreatedVia, "—"), formatMillis(int64(c.CreatedAt))})
			}
			if err := rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "IDENTIFIER", "CREATED VIA", "CREATED"},
				Rows:    rows,
				Raw:     page,
			}); err != nil {
				return err
			}
			hintMoreResults(rt, page)
			return nil
		},
	}
	addListPaginationFlags(cmd, &opts)
	return cmd
}
