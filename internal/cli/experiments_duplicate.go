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

// Creating an experiment takes only the a–d fields.
var duplicateVariantKeys = []string{"a", "b", "c", "d"}

func duplicateExperimentConfig(source *api.Experiment) (api.ExperimentCreate, error) {
	variants, err := withConsecutiveVariantKeys(source)
	if err != nil {
		return api.ExperimentCreate{}, err
	}
	offeringIDs := map[string]string{}
	hasPlacements := false
	for _, variant := range variants {
		if variant.Offering != nil {
			offeringIDs[variant.Key] = variant.Offering.ID
		}
		hasPlacements = hasPlacements || variant.Placements != nil
	}
	if offeringIDs["a"] == "" || offeringIDs["b"] == "" || source.EnrollmentPercent == nil {
		return api.ExperimentCreate{}, fmt.Errorf("experiment %s lacks configuration needed to duplicate it", source.ID)
	}
	// Copying without the conditions would silently widen the copy to every customer.
	if len(source.TargetingConditions) > 0 && source.AudienceID == nil {
		return api.ExperimentCreate{}, fmt.Errorf("experiment %s targets with conditions, which can no longer be copied; create an audience and pass its audience_id to rc experiments create --config", source.ID)
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
	if source.DurationSettings != nil {
		body.ExperimentDurationSettings, _ = json.Marshal(source.DurationSettings)
	}
	if hasPlacements {
		body.Placements, _ = json.Marshal(duplicatePlacementsConfig(variants))
	}
	return body, nil
}

// withConsecutiveVariantKeys renames the source's variants, in the order the
// API returns them, to a, b, c, d: a copy of a, b, d, e becomes a, b, c, d.
func withConsecutiveVariantKeys(source *api.Experiment) ([]api.ExperimentVariant, error) {
	if len(source.Variants) > len(duplicateVariantKeys) {
		return nil, fmt.Errorf("experiment %s has %d variants; duplicate supports at most %d", source.ID, len(source.Variants), len(duplicateVariantKeys))
	}
	variants := make([]api.ExperimentVariant, len(source.Variants))
	for i, variant := range source.Variants {
		variant.Key = duplicateVariantKeys[i]
		variants[i] = variant
	}
	return variants, nil
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
