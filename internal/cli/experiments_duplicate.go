package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
)

func newExperimentsDuplicateCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:     "duplicate [id]",
		Short:   "Copy an experiment into a new draft",
		Long:    "Copies an experiment's configuration into a new draft. Results, status, and timeline are not copied. The new draft does not enroll customers.",
		Example: "  rc experiments duplicate exp123 --name 'New paywall test'",
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
			source, err := client.Experiments.Get(cmd.Context(), projectID, id)
			if err != nil {
				return err
			}
			body, err := duplicateExperimentConfig(source)
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("name") {
				if name == "" {
					return fmt.Errorf("--name cannot be empty")
				}
				body.DisplayName = name
			}
			copy, err := client.Experiments.Create(cmd.Context(), projectID, body)
			if err != nil {
				return err
			}
			rt.Out.Success("Created draft experiment " + copy.ID)
			rt.Out.Hint("rc experiments start " + copy.ID)
			return renderExperimentShow(rt, copy)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "name for the new draft (default: Copy of <source name>)")
	return cmd
}

func duplicateExperimentConfig(source *api.Experiment) (api.ExperimentCreate, error) {
	if source.OfferingA == nil || source.OfferingB == nil || source.EnrollmentPercent == nil {
		return api.ExperimentCreate{}, fmt.Errorf("experiment %s lacks configuration needed to duplicate it", source.ID)
	}
	body := api.ExperimentCreate{
		DisplayName:          "Copy of " + source.DisplayName,
		EnrollmentPercentage: *source.EnrollmentPercent,
		OfferingAID:          source.OfferingA.ID,
		OfferingBID:          source.OfferingB.ID,
		Notes:                source.Notes,
		ExperimentType:       source.ExperimentType,
		PrimaryMetric:        source.PrimaryMetric,
		SecondaryMetrics:     source.SecondaryMetrics,
		EnrollmentMode:       source.EnrollmentMode,
		AudienceID:           source.AudienceID,
	}
	if source.OfferingC != nil {
		body.OfferingCID = &source.OfferingC.ID
	}
	if source.OfferingD != nil {
		body.OfferingDID = &source.OfferingD.ID
	}
	if len(source.TargetingConditions) > 0 && source.AudienceID == nil {
		body.TargetingConditions, _ = json.Marshal(source.TargetingConditions)
	}
	if source.DurationSettings != nil {
		body.ExperimentDurationSettings, _ = json.Marshal(source.DurationSettings)
	}
	if source.Placements != nil {
		placement := map[string]any{}
		for _, variant := range []struct {
			key      string
			offering *api.ExperimentOffering
		}{
			{"fallback_offering_a_id", source.Placements.FallbackOfferingA},
			{"fallback_offering_b_id", source.Placements.FallbackOfferingB},
			{"fallback_offering_c_id", source.Placements.FallbackOfferingC},
			{"fallback_offering_d_id", source.Placements.FallbackOfferingD},
		} {
			if variant.offering != nil {
				placement[variant.key] = variant.offering.ID
			}
		}
		rows := make([]map[string]string, 0, len(source.Placements.PlacementOfferings))
		for _, item := range source.Placements.PlacementOfferings {
			row := map[string]string{"placement_identifier": item.PlacementIdentifier}
			for _, variant := range []struct {
				key      string
				offering *api.ExperimentOffering
			}{
				{"offering_a_id", item.OfferingA},
				{"offering_b_id", item.OfferingB},
				{"offering_c_id", item.OfferingC},
				{"offering_d_id", item.OfferingD},
			} {
				if variant.offering != nil {
					row[variant.key] = variant.offering.ID
				}
			}
			rows = append(rows, row)
		}
		if len(rows) > 0 {
			placement["placement_offerings"] = rows
		}
		body.Placements, _ = json.Marshal(placement)
	}
	return body, nil
}
