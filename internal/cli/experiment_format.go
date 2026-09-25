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
	for _, variant := range []struct {
		name     string
		offering *api.ExperimentOffering
	}{
		{"Control", experiment.OfferingA},
		{"Treatment", experiment.OfferingB},
		{"Treatment C", experiment.OfferingC},
		{"Treatment D", experiment.OfferingD},
	} {
		if variant.offering != nil {
			variants = append(variants, output.CardLine{Key: variant.name, Value: experimentOfferingLabel(variant.offering)})
		}
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
		{Heading: "Placements", Lines: experimentPlacementLines(experiment.Placements), Empty: "No placement overrides"},
		{Heading: "Setup", Lines: measurements},
		{Heading: "Timeline", Lines: timeline},
	}
	if len(experiment.Conflicts) > 0 {
		conflicts := make([]output.CardLine, 0, len(experiment.Conflicts))
		for _, conflict := range experiment.Conflicts {
			name := conflict.DisplayName
			if name == "" {
				name = conflict.ID
			}
			conflicts = append(conflicts, output.CardLine{Key: conflict.ID, Value: name})
		}
		sections = append(sections, output.CardSection{Heading: "Conflicts", Lines: conflicts})
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

func experimentOfferingLabel(offering *api.ExperimentOffering) string {
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

func experimentPlacementLines(placements *api.ExperimentPlacements) []output.CardLine {
	if placements == nil {
		return nil
	}
	lines := []output.CardLine{}
	if fallback := experimentVariantOfferings(placements.FallbackOfferingA, placements.FallbackOfferingB, placements.FallbackOfferingC, placements.FallbackOfferingD); fallback != "" {
		lines = append(lines, output.CardLine{Key: "Fallback", Value: fallback})
	}
	for _, placement := range placements.PlacementOfferings {
		value := experimentVariantOfferings(placement.OfferingA, placement.OfferingB, placement.OfferingC, placement.OfferingD)
		if value == "" {
			value = "Uses variant defaults"
		}
		lines = append(lines, output.CardLine{Key: placement.PlacementIdentifier, Value: value})
	}
	return lines
}

func experimentPlacementSummary(placements *api.ExperimentPlacements) string {
	lines := experimentPlacementLines(placements)
	parts := make([]string, len(lines))
	for i, line := range lines {
		parts[i] = line.Key + ": " + line.Value
	}
	return strings.Join(parts, "; ")
}

func experimentVariantOfferings(a, b, c, d *api.ExperimentOffering) string {
	parts := []string{}
	for _, variant := range []struct {
		name     string
		offering *api.ExperimentOffering
	}{
		{"Control", a}, {"Treatment", b}, {"Treatment C", c}, {"Treatment D", d},
	} {
		if variant.offering != nil {
			parts = append(parts, variant.name+" "+experimentOfferingLabel(variant.offering))
		}
	}
	return strings.Join(parts, " · ")
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
