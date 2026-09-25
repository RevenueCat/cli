package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
)

func newExperimentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "experiments",
		Short: "Run and analyze offering experiments",
		Long:  "Inspect experiments, compare their results, and manage their lifecycle.",
	}
	cmd.AddCommand(newExperimentsListCmd(), newExperimentsShowCmd(), newExperimentsResultsCmd())
	return cmd
}

func newExperimentsListCmd() *cobra.Command {
	var status, cursor string
	var limit int
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List experiments",
		Example: "  rc experiments list --status running\n  rc experiments list --json",
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
			page, err := client.Experiments.List(cmd.Context(), projectID, api.ListExperimentsOptions{
				Status: status, Limit: limit, StartingAfter: cursor,
			})
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(page.Items))
			for _, experiment := range page.Items {
				control, treatment := "", ""
				if experiment.OfferingA != nil {
					control = experiment.OfferingA.ID
				}
				if experiment.OfferingB != nil {
					treatment = experiment.OfferingB.ID
				}
				rows = append(rows, []string{experiment.ID, experiment.DisplayName, experiment.Status, control, treatment})
			}
			if err := rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "NAME", "STATUS", "CONTROL", "TREATMENT"},
				Rows:    rows, Raw: page,
			}); err != nil {
				return err
			}
			hintMoreResults(rt, page)
			return nil
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "filter by draft, running, paused, or stopped")
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum experiments to return (1–100)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "item ID to start after (pagination)")
	return cmd
}

func experimentPickerItems(cmd *cobra.Command, client *api.Client, projectID string) ([]PickerItem, error) {
	page, err := client.Experiments.List(cmd.Context(), projectID, api.ListExperimentsOptions{})
	if err != nil {
		return nil, err
	}
	items := make([]PickerItem, len(page.Items))
	for i, experiment := range page.Items {
		items[i] = PickerItem{ID: experiment.ID, Label: fmt.Sprintf("%s  (%s)", experiment.DisplayName, experiment.Status)}
	}
	return items, nil
}

func newExperimentsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "show [id]",
		Short:   "Show an experiment",
		Example: "  rc experiments show exp123 --json",
		Args:    cobra.MaximumNArgs(1),
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
			id, err := requireID(rt, argAt(args, 0), "experiment", func() ([]PickerItem, error) {
				return experimentPickerItems(cmd, client, projectID)
			})
			if err != nil {
				return err
			}
			experiment, err := client.Experiments.Get(cmd.Context(), projectID, id)
			if err != nil {
				return err
			}
			if err := rt.Out.Render(experiment); err != nil {
				return err
			}
			if !rt.Globals.JSON {
				rt.Out.Hint("Use --json to inspect targeting, placements, and all variant details.")
			}
			return nil
		},
	}
}

func newExperimentsResultsCmd() *cobra.Command {
	var opts api.ExperimentResultsOptions
	cmd := &cobra.Command{
		Use:     "results [id]",
		Short:   "Compare experiment variants and metrics",
		Long:    "Shows total-segment metrics in human output. Use --json for all sections, product segments, and statistics.",
		Example: "  rc experiments results exp123\n  rc experiments results exp123 --json",
		Args:    cobra.MaximumNArgs(1),
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
			id, err := requireID(rt, argAt(args, 0), "experiment", func() ([]PickerItem, error) {
				return experimentPickerItems(cmd, client, projectID)
			})
			if err != nil {
				return err
			}
			results, err := client.Experiments.Results(cmd.Context(), projectID, id, opts)
			if err != nil {
				return err
			}
			if rt.Globals.JSON {
				return rt.Out.Render(results)
			}
			return renderExperimentResults(rt, results)
		},
	}
	cmd.Flags().StringVar(&opts.Platform, "platform", "", "filter by platform")
	cmd.Flags().StringVar(&opts.Country, "country", "", "filter by country code")
	cmd.Flags().StringVar(&opts.ExposureStatus, "exposure-status", "", "filter by exposure status")
	cmd.Flags().StringVar(&opts.Currency, "currency", "", "display currency for monetary metrics")
	return cmd
}

func renderExperimentResults(rt *Runtime, results *api.ExperimentResults) error {
	rt.Out.Title("Experiment results")
	rt.Out.Field("Currency", results.Currency)
	shownSections := 0
	for _, section := range results.Sections {
		rows := make([][]string, 0)
		for _, value := range section.Values {
			if value.Metric < 0 || value.Metric >= len(section.Metrics) || value.Segment < 0 || value.Segment >= len(section.Segments) || !section.Segments[value.Segment].IsTotal {
				continue
			}
			metric := section.Metrics[value.Metric]
			shownValue, change := "—", "—"
			if value.Value != nil {
				shownValue = strconv.FormatFloat(*value.Value, 'f', -1, 64)
			}
			if value.Change != nil {
				change = fmt.Sprintf("%+.2f%%", *value.Change)
			}
			rows = append(rows, []string{metric.Name, value.Variant, shownValue, metric.Unit, change})
		}
		if len(rows) == 0 {
			continue
		}
		shownSections++
		rt.Out.Title(section.Section)
		if err := rt.Out.RenderTable(output.Table{Columns: []string{"METRIC", "VARIANT", "VALUE", "UNIT", "CHANGE"}, Rows: rows}); err != nil {
			return err
		}
	}
	if shownSections == 0 {
		rt.Out.Info("No total-segment metrics available.")
		rt.Out.Hint("Use --json to inspect all segments and statistics.")
	}
	if results.PredictedLTV != nil {
		winner := map[string]string{"a": "Control", "b": "Treatment", "c": "Treatment C", "d": "Treatment D"}[results.PredictedLTV.PredictedWinnerVariant]
		if winner == "" {
			winner = results.PredictedLTV.PredictedWinnerVariant
		}
		rt.Out.Field("Predicted winner", winner)
		rt.Out.Field("Confidence", fmt.Sprintf("%d%%", results.PredictedLTV.Confidence))
	}
	return nil
}
