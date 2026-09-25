package cli

import "github.com/spf13/cobra"

func configFieldsFor(cmd *cobra.Command) map[string]any {
	switch commandPath(cmd) {
	case "rc experiments create":
		return experimentConfigFields(true)
	case "rc experiments update":
		return experimentConfigFields(false)
	case "rc targeting create":
		return targetingConfigFields(true)
	case "rc targeting update":
		return targetingConfigFields(false)
	}
	return nil
}

func targetingConfigFields(create bool) map[string]any {
	schedule := map[string]any{"type": "object", "description": "UTC timestamps ending in Z", "properties": map[string]any{
		"start_date": configField("string", "Start time, for example 2026-05-25T10:00:00Z"),
		"end_date":   configField("string", "Optional end time in the same format"),
	}}
	placements := map[string]any{"type": "object", "description": "Legacy rules only", "properties": map[string]any{
		"fallback_offering_id": configField("string", "Fallback Offering ID"),
		"placement_offerings": map[string]any{"type": "array", "items": map[string]any{
			"type": "object", "required": []string{"placement_identifier", "offering_id"}, "properties": map[string]any{
				"placement_identifier": configField("string", "Placement identifier, such as onboarding"),
				"offering_id":          configField("string", "Offering ID for this placement"),
			},
		}},
	}}
	fields := map[string]any{
		"position":     map[string]any{"type": "integer", "minimum": 1, "description": "One-based priority among rules in the same state; legacy rules only"},
		"state":        configEnum("active or inactive for legacy; checkpoint also supports scheduled", "active", "inactive", "scheduled"),
		"display_name": configField("string", "Rule name"),
		"offering_id":  configField("string", "Offering ID served by a legacy rule"),
		"audience_id":  configField("string", "Audience ID; mutually exclusive with conditions"),
		"conditions":   targetingConditionsSchema(),
		"schedule":     schedule,
		"placements":   placements,
	}
	if create {
		fields["rule_type"] = configEnum("Rule type; defaults to legacy", "legacy", "checkpoint")
		fields["id"] = configField("string", "Optional custom ID for a legacy rule")
		fields["flow_id"] = configField("string", "Flow ID served by a checkpoint rule")
		fields["checkpoints"] = map[string]any{"type": "array", "description": "Exactly one checkpoint for a checkpoint rule", "items": map[string]any{
			"type": "object", "required": []string{"checkpoint_id"}, "properties": map[string]any{
				"checkpoint_id": configField("string", "Checkpoint ID"),
				"position":      map[string]any{"type": "integer", "minimum": 0, "description": "Priority within the checkpoint"},
			},
		}}
		return map[string]any{"type": "object", "properties": fields, "description": "Legacy: display_name and offering_id required. Checkpoint: rule_type, display_name, audience_id, flow_id, checkpoints required."}
	}
	return map[string]any{"type": "object", "properties": fields, "description": "Partial update of a legacy rule. Checkpoint updates are unavailable."}
}

func configField(kind, description string) map[string]any {
	return map[string]any{"type": kind, "description": description}
}

func configEnum(description string, values ...string) map[string]any {
	return map[string]any{"type": "string", "description": description, "enum": values}
}

func experimentConfigFields(create bool) map[string]any {
	metricNames := []string{
		"initial_conversions", "initial_conversion_rate", "trials_started", "trials_completed",
		"trials_converted", "trial_conversion_rate", "paid_customers", "conversion_to_paying",
		"active_subscribers", "churned_subscribers", "refunded_customers", "realized_ltv_revenue",
		"realized_ltv_per_customer", "realized_ltv_per_paying_customer", "total_mrr",
		"mrr_per_customer", "mrr_per_paying_customer",
	}
	placements := map[string]any{
		"type": "object", "description": "Fallback Offerings and per-placement overrides",
		"properties": map[string]any{
			"fallback_offering_a_id": configField("string", "Control Offering ID"),
			"fallback_offering_b_id": configField("string", "Treatment Offering ID"),
			"fallback_offering_c_id": configField("string", "Variant C Offering ID"),
			"fallback_offering_d_id": configField("string", "Variant D Offering ID"),
			"placement_offerings": map[string]any{
				"type": "array", "items": map[string]any{"type": "object", "required": []string{"placement_identifier"}, "properties": map[string]any{
					"placement_identifier": configField("string", "Placement identifier, such as onboarding"),
					"offering_a_id":        configField("string", "Control Offering ID for this placement"),
					"offering_b_id":        configField("string", "Treatment Offering ID for this placement"),
					"offering_c_id":        configField("string", "Variant C Offering ID for this placement"),
					"offering_d_id":        configField("string", "Variant D Offering ID for this placement"),
				}},
			},
		},
	}
	duration := map[string]any{"type": "object", "properties": map[string]any{
		"conversion_rate_percentage":             configField("number", "Expected conversion rate, as a percentage"),
		"daily_enrolled_customers":               configField("integer", "Estimated customers enrolled per day"),
		"minimum_detectable_effect_percentage":   configField("integer", "Minimum detectable effect, as a percentage"),
		"chance_to_win_percentage":               configField("integer", "Required confidence level, as a percentage"),
		"conversion_rate_percentage_is_override": configField("boolean", "Conversion rate was manually overridden"),
		"daily_enrolled_customers_is_override":   configField("boolean", "Daily enrolled count was manually overridden"),
	}}
	secondaryNames := append(append([]string{}, metricNames...), "exposed_customers")
	fields := map[string]any{
		"display_name":                 configField("string", "Experiment name"),
		"enrollment_percentage":        map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "description": "Percentage of eligible customers to enroll"},
		"offering_a_id":                configField("string", "Control Offering ID"),
		"offering_b_id":                configField("string", "Treatment Offering ID"),
		"offering_c_id":                configField("string", "Optional third variant Offering ID"),
		"offering_d_id":                configField("string", "Optional fourth variant Offering ID"),
		"audience_id":                  configField("string", "Audience ID; mutually exclusive with targeting_conditions"),
		"targeting_conditions":         targetingConditionsSchema(),
		"placements":                   placements,
		"notes":                        configField("string", "Experiment notes"),
		"experiment_type":              configEnum("Experiment type", "introductory_offer", "free_trial_offer", "paywall_design", "price_point", "subscription_duration", "subscription_ordering", "other"),
		"primary_metric":               configEnum("Primary metric", metricNames...),
		"secondary_metrics":            map[string]any{"type": "array", "items": configEnum("Secondary metric", secondaryNames...)},
		"enrollment_mode":              configEnum("Enrollment mode", "only_new", "new_and_existing"),
		"experiment_duration_settings": duration,
	}
	if create {
		return map[string]any{"type": "object", "properties": fields, "required": []string{"display_name", "enrollment_percentage", "offering_a_id", "offering_b_id"}}
	}
	return map[string]any{"type": "object", "properties": fields, "description": "Partial update. Running experiments accept only enrollment_percentage; paused experiments cannot be edited."}
}

func targetingConditionsSchema() map[string]any {
	return map[string]any{
		"type": "array", "description": "Mutually exclusive with audience_id. Conditions are combined as an audience filter.",
		"items": map[string]any{
			"type": "object", "required": []string{"field", "operator", "value"},
			"properties": map[string]any{
				"field":    configEnum("Target field", "app_config", "app_version", "country", "custom_attribute", "platform", "sdk_version"),
				"operator": configEnum("Use in/not in for arrays; =, !=, >, >=, <, <= for versions", "in", "not in", "=", "!=", ">", ">=", "<", "<="),
				"value":    configField("string or array of strings", "Version string for app_version/sdk_version; array for other fields"),
				"context":  configField("string", "Required app ID for app_version; SDK flavor for sdk_version; attribute key for custom_attribute. Omit for other fields."),
			},
			"examples": []map[string]any{
				{"field": "platform", "operator": "in", "value": []string{"ios"}},
				{"field": "app_version", "operator": ">=", "value": "1.2.0", "context": "app1a2b3c4"},
			},
			"field_rules": map[string]any{
				"app_config":       map[string]any{"operators": []string{"in", "not in"}, "value": "array of app IDs"},
				"country":          map[string]any{"operators": []string{"in", "not in"}, "value": "array of uppercase two-letter country codes"},
				"platform":         map[string]any{"operators": []string{"in", "not in"}, "value": "array of lowercase platforms", "values": []string{"amazon", "android", "ios", "macos", "roku", "tvos", "visionos", "watchos", "web"}},
				"custom_attribute": map[string]any{"operators": []string{"in", "not in"}, "value": "array of strings or integers", "context": "required attribute key"},
				"app_version":      map[string]any{"operators": []string{"=", "!=", ">", ">=", "<", "<="}, "value": "semantic version string", "context": "required app ID"},
				"sdk_version":      map[string]any{"operators": []string{"=", "!=", ">", ">=", "<", "<="}, "value": "semantic version string", "context": "required SDK flavor, such as ios, android, flutter, or react-native"},
			},
		},
	}
}
