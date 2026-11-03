package cli

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

func newCheckpointsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "checkpoints",
		Aliases: []string{"checkpoint"},
		// Hidden from --help until Checkpoint rules roll out.
		Annotations: map[string]string{annotationSurface: surfaceExperimental},
		Short:       "Manage Checkpoints",
		Long: `A Checkpoint is a point in your app, such as app open or after onboarding,
where Checkpoint rules decide which Flow a customer sees.`,
		Example: `  rc checkpoints list`,
	}
	cmd.AddCommand(newCheckpointsListCmd(), newCheckpointsCreateCmd())
	return cmd
}

func newCheckpointsCreateCmd() *cobra.Command {
	var identifier string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a Checkpoint",
		Long: `Registers the identifier your app will use before the SDK encounters it.
Requires Checkpoint access for the project. Creating a Checkpoint does not
attach or activate a Flow; use targeting rules to choose what it shows.`,
		Example: `  rc checkpoints create --identifier after_onboarding
  rc checkpoints create --identifier after_onboarding --json --no-input`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if err := tui.Form(rt.Globals.NoInput).
				Field(huh.NewInput().Title("Identifier").Value(&identifier).Validate(tui.Required("identifier"))).
				Run(); err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			checkpoint, err := client.Checkpoints.Create(cmd.Context(), projectID, api.CreateCheckpointRequest{Identifier: identifier})
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Created checkpoint %s", checkpoint.ID))
			return rt.Out.Render(checkpoint)
		},
	}
	cmd.Flags().StringVar(&identifier, "identifier", "", "identifier your app uses for the Checkpoint (required)")
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
