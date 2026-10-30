package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
)

func newFlowsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "flows",
		Aliases: []string{"flow"},
		// Hidden from --help until Checkpoint rules roll out.
		Annotations: map[string]string{annotationSurface: surfaceExperimental},
		Short:       "Manage Flows (paywalls, web funnels, and multi-step experiences)",
		Long: `A Flow is a paywall, web funnel, or other multi-step experience built in the
RevenueCat dashboard. Checkpoint rules decide which Flow a customer sees.`,
		Example: `  rc flows list
  rc flows list --type paywall`,
	}
	cmd.AddCommand(newFlowsListCmd())
	cmd.AddCommand(newFlowsShowCmd(), newFlowsExportCmd(), newFlowsUpdateCmd(),
		newFlowsCreateCmd(), newFlowsDuplicateCmd(), newFlowsValidateCmd(), newFlowsPublishCmd())
	return cmd
}

func newFlowsListCmd() *cobra.Command {
	var opts api.FlowListOptions
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Flows",
		Long:  `Lists the project's Flows with their type and whether they are published.`,
		Example: `  rc flows list
  rc flows list --type paywall --type web-funnel
  rc flows list --exclude-type general --json | jq '.data.items[].name'`,
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
			page, err := client.Flows.List(cmd.Context(), projectID, opts)
			if err != nil {
				return err
			}

			rows := make([][]string, 0, len(page.Items))
			for _, f := range page.Items {
				rows = append(rows, []string{f.ID, f.Name, f.Type, flowPublishState(f), formatMillis(int64(f.CreatedAt))})
			}
			if err := rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "NAME", "TYPE", "PUBLISHED", "CREATED"},
				Rows:    rows,
				Raw:     page,
			}); err != nil {
				return err
			}
			hintMoreResults(rt, page)
			return nil
		},
	}
	addListPaginationFlags(cmd, &opts.ListOptions)
	cmd.Flags().StringSliceVar(&opts.Type, "type", nil, "only these types: paywall, web-funnel, general (repeatable)")
	cmd.Flags().StringSliceVar(&opts.ExcludeType, "exclude-type", nil, "every type except these (repeatable)")
	cmd.MarkFlagsMutuallyExclusive("type", "exclude-type")
	return cmd
}

func flowPublishState(f api.Flow) string {
	switch {
	case f.PublishedAt == nil:
		return "never"
	case f.HasUnpublishedChanges:
		return formatMillis(int64(*f.PublishedAt)) + " (draft changes)"
	default:
		return formatMillis(int64(*f.PublishedAt))
	}
}

func flowPickerItems(ctx context.Context, client *api.Client, projectID string) ([]PickerItem, error) {
	page, err := client.Flows.List(ctx, projectID, api.FlowListOptions{})
	if err != nil {
		return nil, err
	}
	items := make([]PickerItem, len(page.Items))
	for i, f := range page.Items {
		items[i] = PickerItem{ID: f.ID, Label: fmt.Sprintf("%s  (%s)", f.Name, f.Type)}
	}
	return items, nil
}
