package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
)

func renderExperimentShow(rt *Runtime, experiment *api.Experiment) error {
	if rt.Globals.JSON {
		return rt.Out.Render(experiment)
	}

	variants := []output.CardLine{}
	for _, variant := range experiment.Variants {
		variants = append(variants, output.CardLine{Key: experimentVariantLabel(variant), Value: experimentOfferingLabel(variant.Offering)})
	}

	audience := []output.CardLine{}
	if experiment.AudienceID != nil && *experiment.AudienceID != "" {
		audience = append(audience, output.CardLine{Key: "Audience", Value: *experiment.AudienceID})
	} else if len(experiment.TargetingConditions) > 0 {
		for i, condition := range experiment.TargetingConditions {
			audience = append(audience, output.CardLine{Key: fmt.Sprintf("Condition %d", i+1), Value: experimentConditionLabel(condition)})
		}
	} else {
		audience = append(audience, output.CardLine{Key: "Audience", Value: "All eligible customers"})
	}
	if experiment.PinnedAudience != nil {
		audience = append(audience, output.CardLine{Key: "Pinned", Value: "Audience saved when enrollment started"})
	}

	measurements := []output.CardLine{}
	if experiment.EnrollmentPercent != nil {
		measurements = append(measurements, output.CardLine{Key: "Enrollment", Value: fmt.Sprintf("%d%%", *experiment.EnrollmentPercent)})
	}
	if experiment.EnrollmentMode != nil {
		measurements = append(measurements, output.CardLine{Key: "Enrollment mode", Value: *experiment.EnrollmentMode})
	}
	if experiment.ExperimentType != nil {
		measurements = append(measurements, output.CardLine{Key: "Type", Value: *experiment.ExperimentType})
	}
	if experiment.PrimaryMetric != nil {
		measurements = append(measurements, output.CardLine{Key: "Primary metric", Value: *experiment.PrimaryMetric})
	}
	if len(experiment.SecondaryMetrics) > 0 {
		measurements = append(measurements, output.CardLine{Key: "Secondary metrics", Value: strings.Join(experiment.SecondaryMetrics, ", ")})
	}
	if experiment.DurationSettings != nil {
		measurements = append(measurements, output.CardLine{Key: "Duration settings", Value: experimentDurationSummary(experiment.DurationSettings)})
	}
	if experiment.Priority != nil {
		measurements = append(measurements, output.CardLine{Key: "Priority", Value: strconv.Itoa(*experiment.Priority)})
	}
	if experiment.Notes != nil && *experiment.Notes != "" {
		measurements = append(measurements, output.CardLine{Key: "Notes", Value: *experiment.Notes})
	}

	timeline := []output.CardLine{}
	type timelineEvent struct {
		name string
		at   api.Millis
	}
	events := []timelineEvent{}
	for _, event := range []struct {
		name string
		at   *api.Millis
	}{
		{"Started", experiment.StartedAt},
		{"Paused", experiment.PausedAt},
		{"Resumed", experiment.ResumedAt},
		{"Stopped", experiment.StoppedAt},
	} {
		if event.at != nil {
			events = append(events, timelineEvent{name: event.name, at: *event.at})
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].at < events[j].at })
	for _, event := range events {
		timeline = append(timeline, output.CardLine{Key: event.name, Value: formatMillis(int64(event.at))})
	}
	if experiment.TotalRunningSeconds != nil {
		timeline = append(timeline, output.CardLine{Key: "Running time", Value: formatExperimentRunTime(*experiment.TotalRunningSeconds)})
	}
	if len(timeline) == 0 {
		timeline = append(timeline, output.CardLine{Key: "Created", Value: formatMillis(int64(experiment.CreatedAt))})
	}

	sections := []output.CardSection{
		{Heading: "Variants", Lines: variants},
		{Heading: "Audience", Lines: audience},
		{Heading: "Placements", Lines: experimentPlacementLines(experiment.Variants), Empty: "No placement overrides"},
		{Heading: "Setup", Lines: measurements},
		{Heading: "Timeline", Lines: timeline},
	}
	if len(experiment.Conflicts) > 0 {
		sections = append(sections, output.CardSection{Heading: "Conflicts", Lines: []output.CardLine{{Key: "Experiments", Value: experimentConflictsSummary(experiment.Conflicts)}}})
	}
	if err := rt.Out.RenderCard(output.Card{
		Title:    experiment.DisplayName,
		Subtitle: experiment.ID + " · " + experiment.Status,
		Sections: sections,
		Raw:      experiment,
	}); err != nil {
		return err
	}
	rt.Out.Hint("Use --json for the complete API response.")
	return nil
}

func experimentVariantLabel(variant api.ExperimentVariant) string {
	if variant.IsControl {
		return "Control (" + variant.Key + ")"
	}
	return "Variant " + variant.Key
}

func experimentOfferingLabel(offering *api.ExperimentOffering) string {
	if offering == nil {
		return "No offering"
	}
	label := offering.ID
	if offering.DisplayName != "" {
		label += " (" + offering.DisplayName + ")"
	}
	if offering.PaywallID != "" {
		label += " · paywall " + offering.PaywallID
	}
	return label
}

func experimentConditionLabel(condition api.ExperimentCondition) string {
	field := strings.ReplaceAll(condition.Field, "_", " ")
	if condition.Context != nil {
		field += " (" + fmt.Sprint(condition.Context) + ")"
	}
	value := fmt.Sprint(condition.Value)
	if values, ok := condition.Value.([]any); ok {
		parts := make([]string, len(values))
		for i, item := range values {
			parts[i] = fmt.Sprint(item)
		}
		value = strings.Join(parts, ", ")
	}
	return field + " " + condition.Operator + " " + value
}

func experimentConditionSummary(conditions []api.ExperimentCondition) string {
	parts := make([]string, len(conditions))
	for i, condition := range conditions {
		parts[i] = experimentConditionLabel(condition)
	}
	return strings.Join(parts, "; ")
}

func experimentPlacementLines(variants []api.ExperimentVariant) []output.CardLine {
	fallback := []string{}
	byPlacement := map[string][]string{}
	placementOrder := []string{}
	for _, variant := range variants {
		if variant.Placements == nil {
			continue
		}
		label := experimentVariantLabel(variant)
		if variant.Placements.FallbackOffering != nil {
			fallback = append(fallback, label+" "+experimentOfferingLabel(variant.Placements.FallbackOffering))
		}
		for _, placement := range variant.Placements.PlacementOfferings {
			if _, seen := byPlacement[placement.PlacementIdentifier]; !seen {
				placementOrder = append(placementOrder, placement.PlacementIdentifier)
			}
			// A listed placement without an offering shows no paywall.
			value := "no paywall"
			if placement.Offering != nil {
				value = experimentOfferingLabel(placement.Offering)
			}
			byPlacement[placement.PlacementIdentifier] = append(byPlacement[placement.PlacementIdentifier], label+" "+value)
		}
	}
	lines := []output.CardLine{}
	if len(fallback) > 0 {
		lines = append(lines, output.CardLine{Key: "Fallback", Value: strings.Join(fallback, " · ")})
	}
	for _, identifier := range placementOrder {
		lines = append(lines, output.CardLine{Key: identifier, Value: strings.Join(byPlacement[identifier], " · ")})
	}
	return lines
}

func experimentDurationSummary(settings *api.ExperimentDurationSettings) string {
	parts := []string{}
	if settings.ConversionRatePercentage != nil {
		parts = append(parts, fmt.Sprintf("expected conversion %.2f%%", *settings.ConversionRatePercentage))
	}
	if settings.DailyEnrolledCustomers != nil {
		parts = append(parts, fmt.Sprintf("%d customers/day", *settings.DailyEnrolledCustomers))
	}
	if settings.MinimumDetectableEffectPercentage != nil {
		parts = append(parts, fmt.Sprintf("minimum effect %d%%", *settings.MinimumDetectableEffectPercentage))
	}
	if settings.ChanceToWinPercentage != nil {
		parts = append(parts, fmt.Sprintf("chance to win %d%%", *settings.ChanceToWinPercentage))
	}
	if len(parts) == 0 {
		return "Configured"
	}
	return strings.Join(parts, " · ")
}

func experimentConflictsSummary(conflicts []api.ExperimentConflict) string {
	parts := make([]string, len(conflicts))
	for i, conflict := range conflicts {
		parts[i] = conflict.DisplayName
		if parts[i] == "" {
			parts[i] = conflict.ID
		} else if conflict.ID != "" {
			parts[i] += " (" + conflict.ID + ")"
		}
	}
	return strings.Join(parts, ", ")
}

func formatExperimentRunTime(seconds int) string {
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	days := seconds / 86400
	hours := seconds % 86400 / 3600
	minutes := seconds % 3600 / 60
	parts := []string{}
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 || days > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	return strings.Join(parts, " ")
}
