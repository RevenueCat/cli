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
	offeringIDs := map[string]string{}
	hasPlacements := false
	for _, variant := range source.Variants {
		// Creating an experiment still takes the a–d fields, so a later key has nowhere to go.
		switch variant.Key {
		case "a", "b", "c", "d":
		default:
			return api.ExperimentCreate{}, fmt.Errorf("experiment %s has variant %s; duplicate supports only variants a to d", source.ID, variant.Key)
		}
		if variant.Offering != nil {
			offeringIDs[variant.Key] = variant.Offering.ID
		}
		hasPlacements = hasPlacements || variant.Placements != nil
	}
	if offeringIDs["a"] == "" || offeringIDs["b"] == "" || source.EnrollmentPercent == nil {
		return api.ExperimentCreate{}, fmt.Errorf("experiment %s lacks configuration needed to duplicate it", source.ID)
	}
	body := api.ExperimentCreate{
		DisplayName:          "Copy of " + source.DisplayName,
		EnrollmentPercentage: *source.EnrollmentPercent,
		OfferingAID:          offeringIDs["a"],
		OfferingBID:          offeringIDs["b"],
		Notes:                source.Notes,
		ExperimentType:       source.ExperimentType,
		PrimaryMetric:        source.PrimaryMetric,
		SecondaryMetrics:     source.SecondaryMetrics,
		EnrollmentMode:       source.EnrollmentMode,
		AudienceID:           source.AudienceID,
	}
	if id, ok := offeringIDs["c"]; ok {
		body.OfferingCID = &id
	}
	if id, ok := offeringIDs["d"]; ok {
		body.OfferingDID = &id
	}
	if len(source.TargetingConditions) > 0 && source.AudienceID == nil {
		body.TargetingConditions, _ = json.Marshal(source.TargetingConditions)
	}
	if source.DurationSettings != nil {
		body.ExperimentDurationSettings, _ = json.Marshal(source.DurationSettings)
	}
	if hasPlacements {
		body.Placements, _ = json.Marshal(duplicatePlacementsConfig(source.Variants))
	}
	return body, nil
}

func duplicatePlacementsConfig(variants []api.ExperimentVariant) map[string]any {
	placements := map[string]any{}
	rows := []map[string]string{}
	rowByPlacement := map[string]map[string]string{}
	for _, variant := range variants {
		if variant.Placements == nil {
			continue
		}
		if variant.Placements.FallbackOffering != nil {
			placements["fallback_offering_"+variant.Key+"_id"] = variant.Placements.FallbackOffering.ID
		}
		for _, item := range variant.Placements.PlacementOfferings {
			row, seen := rowByPlacement[item.PlacementIdentifier]
			if !seen {
				row = map[string]string{"placement_identifier": item.PlacementIdentifier}
				rowByPlacement[item.PlacementIdentifier] = row
				rows = append(rows, row)
			}
			if item.Offering != nil {
				row["offering_"+variant.Key+"_id"] = item.Offering.ID
			}
		}
	}
	if len(rows) > 0 {
		placements["placement_offerings"] = rows
	}
	return placements
}
