package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
)

func targetingConfigSummary(key string, raw json.RawMessage) (string, error) {
	if key == "conditions" {
		var conditions []api.ExperimentCondition
		if err := json.Unmarshal(raw, &conditions); err != nil {
			return "", fmt.Errorf("conditions must be an array: %w", err)
		}
		if len(conditions) == 0 {
			return "Everyone", nil
		}
		return experimentConditionSummary(conditions), nil
	}
	if key == "checkpoints" {
		var checkpoints []struct {
			ID string `json:"checkpoint_id"`
		}
		if err := json.Unmarshal(raw, &checkpoints); err != nil {
			return "", err
		}
		ids := make([]string, len(checkpoints))
		for i, checkpoint := range checkpoints {
			ids[i] = checkpoint.ID
		}
		return strings.Join(ids, ", "), nil
	}
	if key == "schedule" {
		if string(raw) == "null" {
			return "None", nil
		}
		var schedule struct {
			StartDate *string `json:"start_date"`
			EndDate   *string `json:"end_date"`
		}
		if err := json.Unmarshal(raw, &schedule); err != nil {
			return "", err
		}
		parts := make([]string, 0, 2)
		if schedule.StartDate != nil {
			parts = append(parts, "start: "+*schedule.StartDate)
		}
		if schedule.EndDate != nil {
			parts = append(parts, "end: "+*schedule.EndDate)
		}
		return strings.Join(parts, " · "), nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	return experimentChangeValue(value), nil
}

func targetingConditionsSummary(conditions []any) (string, error) {
	raw, err := json.Marshal(conditions)
	if err != nil {
		return "", err
	}
	return targetingConfigSummary("conditions", raw)
}

func showTargetingUpdateChanges(rt *Runtime, changes api.TargetingRuleUpdate) error {
	keys := make([]string, 0, len(changes))
	for key := range changes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value, err := targetingConfigSummary(key, changes[key])
		if err != nil {
			return err
		}
		rt.Out.Field("Change "+strings.ReplaceAll(key, "_", " "), value)
	}
	return nil
}

type targetingDisplay struct {
	Conditions []api.ExperimentCondition `json:"conditions"`
	Schedule   *struct {
		StartDate *string `json:"start_date"`
		EndDate   *string `json:"end_date"`
	} `json:"schedule"`
	Placements *struct {
		FallbackOfferingID *string `json:"fallback_offering_id"`
		PlacementOfferings []struct {
			PlacementIdentifier string  `json:"placement_identifier"`
			OfferingID          *string `json:"offering_id"`
		} `json:"placement_offerings"`
	} `json:"placements"`
	Checkpoints []struct {
		CheckpointID string `json:"checkpoint_id"`
		Position     int    `json:"position"`
		Checkpoint   *struct {
			Identifier string `json:"identifier"`
		} `json:"checkpoint"`
	} `json:"checkpoints"`
}

func renderTargetingShow(rt *Runtime, rule *api.TargetingRule) error {
	if rt.Globals.JSON {
		return rt.Out.Render(rule)
	}

	data, err := json.Marshal(rule)
	if err != nil {
		return err
	}
	var view targetingDisplay
	if err := json.Unmarshal(data, &view); err != nil {
		return err
	}

	serves := []output.CardLine{}
	if rule.OfferingID != "" {
		serves = append(serves, output.CardLine{Key: "Offering", Value: rule.OfferingID})
	}
	if rule.FlowID != "" {
		serves = append(serves, output.CardLine{Key: "Flow", Value: rule.FlowID})
	}

	audience := []output.CardLine{}
	if rule.AudienceID != nil && *rule.AudienceID != "" {
		audience = append(audience, output.CardLine{Key: "Audience", Value: *rule.AudienceID})
	} else if len(view.Conditions) > 0 {
		for i, condition := range view.Conditions {
			audience = append(audience, output.CardLine{Key: fmt.Sprintf("Condition %d", i+1), Value: experimentConditionLabel(condition)})
		}
	} else {
		audience = append(audience, output.CardLine{Key: "Audience", Value: "All eligible customers"})
	}

	sections := []output.CardSection{
		{Heading: "Serves", Lines: serves},
		{Heading: "Audience", Lines: audience},
	}
	if rule.RuleType == "legacy" {
		placements := []output.CardLine{}
		if view.Placements != nil {
			if view.Placements.FallbackOfferingID != nil {
				placements = append(placements, output.CardLine{Key: "Fallback", Value: *view.Placements.FallbackOfferingID})
			}
			for _, placement := range view.Placements.PlacementOfferings {
				offering := "Use fallback"
				if placement.OfferingID != nil {
					offering = *placement.OfferingID
				}
				placements = append(placements, output.CardLine{Key: placement.PlacementIdentifier, Value: offering})
			}
		}
		sections = append(sections, output.CardSection{Heading: "Placements", Lines: placements, Empty: "No placement overrides"})
	}
	if view.Schedule != nil {
		schedule := []output.CardLine{}
		if view.Schedule.StartDate != nil {
			schedule = append(schedule, output.CardLine{Key: "Start", Value: *view.Schedule.StartDate})
		}
		if view.Schedule.EndDate != nil {
			schedule = append(schedule, output.CardLine{Key: "End", Value: *view.Schedule.EndDate})
		}
		sections = append(sections, output.CardSection{Heading: "Schedule", Lines: schedule})
	}
	if rule.RuleType == "checkpoint" {
		checkpoints := []output.CardLine{}
		for _, checkpoint := range view.Checkpoints {
			name := checkpoint.CheckpointID
			if checkpoint.Checkpoint != nil && checkpoint.Checkpoint.Identifier != "" {
				name = checkpoint.Checkpoint.Identifier + " (" + checkpoint.CheckpointID + ")"
			}
			checkpoints = append(checkpoints, output.CardLine{Key: name, Value: fmt.Sprintf("Position %d", checkpoint.Position)})
		}
		sections = append(sections, output.CardSection{Heading: "Checkpoints", Lines: checkpoints})
	}
	if err := rt.Out.RenderCard(output.Card{
		Title:    rule.DisplayName,
		Subtitle: rule.ID + " · " + rule.RuleType + " · " + rule.State,
		Sections: sections,
		Raw:      rule,
	}); err != nil {
		return err
	}
	rt.Out.Hint("Use --json for the complete API response.")
	return nil
}
