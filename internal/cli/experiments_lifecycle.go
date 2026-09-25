package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

func newExperimentsCreateCmd() *cobra.Command {
	var name, control, treatment, config string
	var enrollment int
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a draft experiment",
		Long:  "Creates a draft comparing two Offerings. Use --config for targeting, placements, extra variants, and metrics. Creating a draft does not enroll customers.",
		Example: `  rc experiments create --name "New paywall" --control ofrng_a --treatment ofrng_b --enrollment 50
  rc experiments create --config experiment.json --json --no-input`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			body := api.ExperimentCreate{}
			if config != "" {
				if err := readJSONConfig(config, &body); err != nil {
					return err
				}
			}
			if cmd.Flags().Changed("name") {
				body.DisplayName = name
			}
			if cmd.Flags().Changed("control") {
				body.OfferingAID = control
			}
			if cmd.Flags().Changed("treatment") {
				body.OfferingBID = treatment
			}
			if cmd.Flags().Changed("enrollment") {
				body.EnrollmentPercentage = enrollment
			}
			if !rt.CanPrompt() {
				var missing []string
				if body.DisplayName == "" {
					missing = append(missing, "--name")
				}
				if body.OfferingAID == "" {
					missing = append(missing, "--control")
				}
				if body.OfferingBID == "" {
					missing = append(missing, "--treatment")
				}
				if body.EnrollmentPercentage == 0 {
					missing = append(missing, "--enrollment")
				}
				if len(missing) > 0 {
					return fmt.Errorf("experiment input required: pass %s or --config <file>", strings.Join(missing, ", "))
				}
			} else {
				form := tui.Form(false)
				if body.DisplayName == "" {
					form.Field(huh.NewInput().Title("Experiment name").Value(&body.DisplayName).Validate(tui.Required("name")))
				}
				if body.OfferingAID == "" {
					form.Field(huh.NewInput().Title("Control offering ID").Value(&body.OfferingAID).Validate(tui.Required("control offering")))
				}
				if body.OfferingBID == "" {
					form.Field(huh.NewInput().Title("Treatment offering ID").Value(&body.OfferingBID).Validate(tui.Required("treatment offering")))
				}
				if body.EnrollmentPercentage == 0 {
					percent := "50"
					form.Field(huh.NewInput().Title("Enrollment percentage (1–100)").Value(&percent))
					if err := form.Run(); err != nil {
						return err
					}
					body.EnrollmentPercentage, err = strconv.Atoi(percent)
					if err != nil {
						return fmt.Errorf("enrollment must be an integer from 1 to 100")
					}
				} else if err := form.Run(); err != nil {
					return err
				}
			}
			if body.EnrollmentPercentage < 1 || body.EnrollmentPercentage > 100 {
				return fmt.Errorf("enrollment must be an integer from 1 to 100")
			}
			if body.AudienceID != nil && len(body.TargetingConditions) > 0 {
				return fmt.Errorf("audience_id and targeting_conditions cannot both be set")
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			experiment, err := client.Experiments.Create(cmd.Context(), projectID, body)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Created draft experiment %s", experiment.ID))
			rt.Out.Hint("rc experiments start " + experiment.ID)
			return rt.Out.Render(experiment)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "experiment display name")
	cmd.Flags().StringVar(&control, "control", "", "control Offering ID")
	cmd.Flags().StringVar(&treatment, "treatment", "", "treatment Offering ID")
	cmd.Flags().IntVar(&enrollment, "enrollment", 0, "percentage of eligible customers to enroll (1–100)")
	cmd.Flags().StringVar(&config, "config", "", "JSON config file; use - for stdin")
	return cmd
}

func newExperimentsUpdateCmd() *cobra.Command {
	var config string
	cmd := &cobra.Command{
		Use:     "update [id]",
		Short:   "Update an experiment",
		Long:    "Partially updates an experiment from a JSON object. Only supplied fields change. A running experiment requires confirmation.",
		Example: `  rc experiments update exp123 --config changes.json --yes --no-input`,
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if config == "" {
				return fmt.Errorf("pass --config <file> or --config - for stdin")
			}
			body := api.ExperimentUpdate{}
			if err := readJSONConfig(config, &body); err != nil {
				return err
			}
			if len(body) == 0 {
				return fmt.Errorf("config must contain at least one field")
			}
			if err := validExperimentUpdate(body); err != nil {
				return err
			}
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
			current, err := client.Experiments.Get(cmd.Context(), projectID, id)
			if err != nil {
				return err
			}
			if current.Status == "running" {
				rt.Out.Notice("This experiment is running. Changes may affect customers being enrolled now.")
				if err := confirmOrAbort(rt, "Update running experiment now?"); err != nil {
					return err
				}
			}
			experiment, err := client.Experiments.Update(cmd.Context(), projectID, id, body)
			if err != nil {
				return err
			}
			rt.Out.Success("Updated experiment " + experiment.ID)
			return rt.Out.Render(experiment)
		},
	}
	cmd.Flags().StringVar(&config, "config", "", "JSON object of fields to update; use - for stdin")
	return cmd
}

func newExperimentsStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start [id]",
		Short: "Start a draft experiment",
		Long:  "Starts enrollment for the experiment after showing its current configuration. Requires confirmation or --yes.",
		Args:  cobra.MaximumNArgs(1),
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
			current, err := client.Experiments.Get(cmd.Context(), projectID, id)
			if err != nil {
				return err
			}
			if current.Status != "draft" {
				return fmt.Errorf("experiment %s is %s; only drafts can be started", id, current.Status)
			}
			rt.Out.Title("Experiment — " + current.DisplayName)
			rt.Out.Lead("Start enrolling eligible customers in the experiment.")
			rt.Out.Field("Status", current.Status)
			if current.OfferingA != nil {
				rt.Out.Field("Control", current.OfferingA.ID)
			}
			if current.OfferingB != nil {
				rt.Out.Field("Treatment", current.OfferingB.ID)
			}
			if current.EnrollmentPercent != nil {
				rt.Out.Field("Enrollment", fmt.Sprintf("%d%%", *current.EnrollmentPercent))
			}
			rt.Out.Plan([]string{"Start the experiment and enroll eligible customers"})
			if err := confirmOrAbort(rt, "Start experiment now?"); err != nil {
				return err
			}
			rt.Out.Info("Starting experiment…")
			experiment, err := client.Experiments.Start(cmd.Context(), projectID, id)
			if err != nil {
				return err
			}
			rt.Out.Success("Started experiment " + experiment.ID)
			if err := rt.Out.RenderCard(output.Card{
				Title:    experiment.DisplayName,
				Sections: []output.CardSection{{Lines: []output.CardLine{{Key: "ID", Value: experiment.ID}, {Key: "Status", Value: experiment.Status}}}},
				Raw:      experiment,
			}); err != nil {
				return err
			}
			rt.Out.Hint("rc experiments results " + experiment.ID)
			return nil
		},
	}
}

func experimentActionCmd(action string, run func(*api.ExperimentsService, *cobra.Command, string, string) (*api.Experiment, error)) *cobra.Command {
	return &cobra.Command{
		Use:   action + " [id]",
		Short: strings.ToUpper(action[:1]) + action[1:] + " an experiment",
		Args:  cobra.MaximumNArgs(1),
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
			if action == "stop" {
				if err := confirmOrAbort(rt, "Permanently stop experiment "+id+"?"); err != nil {
					return err
				}
			}
			experiment, err := run(client.Experiments, cmd, projectID, id)
			if err != nil {
				return err
			}
			past := map[string]string{"pause": "Paused", "resume": "Resumed", "stop": "Stopped"}[action]
			rt.Out.Success(past + " experiment " + experiment.ID)
			return rt.Out.Render(experiment)
		},
	}
}

func newExperimentsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete [id]",
		Short: "Delete a draft experiment",
		Args:  cobra.MaximumNArgs(1),
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
			if err := confirmOrAbort(rt, "Delete draft experiment "+id+"?"); err != nil {
				return err
			}
			if err := client.Experiments.Delete(cmd.Context(), projectID, id); err != nil {
				return err
			}
			rt.Out.Success("Deleted experiment " + id)
			return rt.Out.Render(map[string]any{"id": id, "deleted": true})
		},
	}
}

func validExperimentUpdate(body api.ExperimentUpdate) error {
	allowed := map[string]bool{
		"display_name": true, "enrollment_percentage": true, "offering_a_id": true,
		"offering_b_id": true, "offering_c_id": true, "offering_d_id": true,
		"targeting_conditions": true, "audience_id": true, "placements": true,
		"notes": true, "experiment_type": true, "primary_metric": true,
		"secondary_metrics": true, "enrollment_mode": true, "experiment_duration_settings": true,
	}
	for key, value := range body {
		if !allowed[key] {
			return fmt.Errorf("unknown experiment field %q", key)
		}
		if !json.Valid(value) {
			return fmt.Errorf("invalid JSON value for %q", key)
		}
	}
	return nil
}
