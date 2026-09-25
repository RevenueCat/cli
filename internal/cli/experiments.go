package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

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
	cmd.AddCommand(
		newExperimentsListCmd(), newExperimentsShowCmd(), newExperimentsResultsCmd(),
		newExperimentsCreateCmd(), newExperimentsUpdateCmd(), newExperimentsDeleteCmd(),
		newExperimentsStartCmd(),
		experimentActionCmd("pause", func(s *api.ExperimentsService, cmd *cobra.Command, projectID, id string) (*api.Experiment, error) {
			return s.Pause(cmd.Context(), projectID, id)
		}),
		experimentActionCmd("resume", func(s *api.ExperimentsService, cmd *cobra.Command, projectID, id string) (*api.Experiment, error) {
			return s.Resume(cmd.Context(), projectID, id)
		}),
		experimentActionCmd("stop", func(s *api.ExperimentsService, cmd *cobra.Command, projectID, id string) (*api.Experiment, error) {
			return s.Stop(cmd.Context(), projectID, id)
		}),
	)
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
			return renderExperimentShow(rt, experiment)
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
			platform, knownPlatform := experimentResultsPlatform(opts.Platform)
			opts.Platform = platform
			if !knownPlatform {
				rt.Out.AlwaysWarn(fmt.Sprintf("Unknown platform %q; sending it as provided. Results may be empty.", platform))
			}
			var err error
			opts.ExposureStatus, err = experimentExposureStatus(opts.ExposureStatus)
			if err != nil {
				return err
			}
			opts.Country = strings.ToUpper(strings.TrimSpace(opts.Country))
			opts.Currency = strings.ToUpper(strings.TrimSpace(opts.Currency))
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
	cmd.Flags().StringVar(&opts.Platform, "platform", "", "filter by iOS, Android, macOS, tvOS, watchOS, visionOS, Amazon, Roku, or Web (case-insensitive; app_store/play_store aliases); other values pass through with a warning")
	cmd.Flags().StringVar(&opts.Country, "country", "", "filter by ISO country code (for example, US)")
	cmd.Flags().StringVar(&opts.ExposureStatus, "exposure-status", "", "filter by exposed or not_exposed; omit for all enrolled customers")
	cmd.Flags().StringVar(&opts.Currency, "currency", "", "display currency for monetary metrics")
	return cmd
}

func experimentResultsPlatform(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", true
	}
	switch strings.ToLower(value) {
	case "ios", "app_store":
		return "iOS", true
	case "android", "play_store":
		return "Android", true
	case "macos", "mac_app_store":
		return "macOS", true
	case "tvos":
		return "tvOS", true
	case "watchos":
		return "watchOS", true
	case "visionos":
		return "visionOS", true
	case "amazon":
		return "Amazon", true
	case "roku":
		return "Roku", true
	case "web":
		return "Web", true
	default:
		return value, false
	}
}

func experimentExposureStatus(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "exposed":
		return "exposed", nil
	case "not_exposed":
		return "not_exposed", nil
	default:
		return "", fmt.Errorf("exposure status must be exposed or not_exposed")
	}
}

func renderExperimentResults(rt *Runtime, results *api.ExperimentResults) error {
	rt.Out.Title("Experiment results")
	rt.Out.Field("Currency", results.Currency)
	shownSections := 0
	for _, section := range results.Sections {
		valuesByMetric := make(map[int]map[string]api.ExperimentResultValue)
		hasTreatmentC, hasTreatmentD := false, false
		for _, value := range section.Values {
			if value.Metric < 0 || value.Metric >= len(section.Metrics) || value.Segment < 0 || value.Segment >= len(section.Segments) || !section.Segments[value.Segment].IsTotal {
				continue
			}
			if valuesByMetric[value.Metric] == nil {
				valuesByMetric[value.Metric] = make(map[string]api.ExperimentResultValue)
			}
			valuesByMetric[value.Metric][value.Variant] = value
			hasTreatmentC = hasTreatmentC || value.Variant == "TreatmentC"
			hasTreatmentD = hasTreatmentD || value.Variant == "TreatmentD"
		}
		if len(valuesByMetric) == 0 {
			continue
		}
		columns := []string{"METRIC", "CONTROL", "TREATMENT", "CHANGE"}
		if hasTreatmentC {
			columns = append(columns, "TREATMENT C", "CHANGE C")
		}
		if hasTreatmentD {
			columns = append(columns, "TREATMENT D", "CHANGE D")
		}
		columns = append(columns, "UNIT")
		rows := make([][]string, 0, len(valuesByMetric))
		for i, metric := range section.Metrics {
			values := valuesByMetric[i]
			if len(values) == 0 {
				continue
			}
			row := []string{metric.Name, resultValue(values["Control"]), resultValue(values["Treatment"]), resultChange(values["Treatment"])}
			if hasTreatmentC {
				row = append(row, resultValue(values["TreatmentC"]), resultChange(values["TreatmentC"]))
			}
			if hasTreatmentD {
				row = append(row, resultValue(values["TreatmentD"]), resultChange(values["TreatmentD"]))
			}
			row = append(row, metric.Unit)
			rows = append(rows, row)
		}
		shownSections++
		rt.Out.Title(section.Section)
		if err := rt.Out.RenderTable(output.Table{Columns: columns, Rows: rows}); err != nil {
			return err
		}
	}
	if shownSections == 0 {
		rt.Out.Info("No total-segment metrics available.")
		rt.Out.Hint("Use --json to inspect all segments and statistics.")
	}
	statistics := [][]string{}
	for _, metric := range results.Statistics {
		for _, raw := range metric.Variants {
			var variant api.ExperimentResultVariantStatistic
			if err := json.Unmarshal(raw, &variant); err != nil {
				return err
			}
			if variant.Name == "Control" {
				continue
			}
			statistics = append(statistics, []string{
				metric.MetricName,
				variant.Name,
				chanceToWinLabel(variant),
				liftIntervalLabel(variant),
			})
		}
	}
	if len(statistics) > 0 {
		rt.Out.Title("Decision signals")
		if err := rt.Out.RenderTable(output.Table{Columns: []string{"METRIC", "VARIANT", "CHANCE TO WIN", "95% LIFT INTERVAL"}, Rows: statistics}); err != nil {
			return err
		}
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

func chanceToWinLabel(stat api.ExperimentResultVariantStatistic) string {
	if stat.ChanceToWin != nil {
		return fmt.Sprintf("%.1f%%", *stat.ChanceToWin*100)
	}
	if stat.ChanceToWinStatus == "insufficient_data" {
		return "Need more data"
	}
	return "—"
}

func liftIntervalLabel(stat api.ExperimentResultVariantStatistic) string {
	if stat.LiftCredibleIntervalLower != nil && stat.LiftCredibleIntervalUpper != nil {
		return fmt.Sprintf("%+.1f%% to %+.1f%%", *stat.LiftCredibleIntervalLower*100, *stat.LiftCredibleIntervalUpper*100)
	}
	if stat.LiftCredibleIntervalStatus == "insufficient_data" {
		return "Need more data"
	}
	return "—"
}

func resultValue(value api.ExperimentResultValue) string {
	if value.Value == nil {
		return "—"
	}
	return strconv.FormatFloat(*value.Value, 'f', -1, 64)
}

func resultChange(value api.ExperimentResultValue) string {
	if value.Change == nil {
		return "—"
	}
	return fmt.Sprintf("%+.2f%%", *value.Change)
}
