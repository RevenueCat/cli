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
	schedule := map[string]any{"type": "object", "description": "UTC timestamps ending in Z. Legacy Offering rules require start_date; set state to active with a future start_date to serve later.", "required_for_legacy": []string{"start_date"}, "properties": map[string]any{
		"start_date": map[string]any{"type": []string{"string", "null"}, "description": "Start time in UTC, for example 2026-05-25T10:00:00Z. Null means no start date for a checkpoint rule; Offering schedules require a start date."},
		"end_date":   map[string]any{"type": []string{"string", "null"}, "description": "End time in UTC; omit or set null for no end date."},
	}}
	placements := map[string]any{"type": []string{"object", "null"}, "description": "Legacy rules only. On update, omit to keep overrides or set null to remove them; both fields are required when providing an object.", "required": []string{"fallback_offering_id", "placement_offerings"}, "properties": map[string]any{
		"fallback_offering_id": map[string]any{"type": []string{"string", "null"}, "description": "Fallback Offering ID; null means no fallback."},
		"placement_offerings": map[string]any{"type": "array", "items": map[string]any{
			"type": "object", "required": []string{"placement_identifier", "offering_id"}, "properties": map[string]any{
				"placement_identifier": configField("string", "Placement identifier, such as onboarding"),
				"offering_id":          map[string]any{"type": []string{"string", "null"}, "description": "Offering ID for this placement; null means no Offering override."},
			},
		}},
	}}
	fields := map[string]any{
		"position":     map[string]any{"type": "integer", "minimum": 1, "description": "One-based priority among rules in the same state; legacy rules only. List preserves evaluation order, but the API does not return absolute positions."},
		"state":        configEnum("Legacy Offering rules use active or inactive; use active with a future schedule.start_date to serve later. The scheduled state is checkpoint-only.", "active", "inactive", "scheduled"),
		"display_name": configField("string", "Rule name"),
		"offering_id":  configField("string", "Offering ID served by a legacy rule"),
		"audience_id":  map[string]any{"type": []string{"string", "null"}, "description": "Audience ID; mutually exclusive with conditions. Set null when switching to conditions; set conditions to [] when switching to an audience. Checkpoint rules require a non-null audience ID."},
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
	schedule["type"] = []string{"object", "null"}
	schedule["description"] = "Omit to keep the current schedule. Set null to remove it, or provide UTC timestamps. Offering rules require start_date and state active to serve later; checkpoint rules allow an omitted start_date unless state is scheduled."
	fields["flow_id"] = configField("string", "Flow ID served by a checkpoint rule; checkpoint-only")
	fields["checkpoints"] = map[string]any{
		"type": "array", "minItems": 1, "maxItems": 1,
		"description": "Move a checkpoint rule to exactly one checkpoint; appends after that checkpoint's existing rules. Position cannot be set here.",
		"items": map[string]any{
			"type": "object", "required": []string{"checkpoint_id"}, "additionalProperties": false,
			"properties": map[string]any{"checkpoint_id": configField("string", "Checkpoint ID")},
		},
	}
	return map[string]any{
		"type": "object", "properties": fields,
		"description": "Partial update. Active or scheduled rules require confirmation. Checkpoint rules accept state, display_name, audience_id (non-null), flow_id, checkpoints, and schedule; Offering fields and position are legacy-only.",
	}
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
	audience := map[string]any{
		"type":        []string{"string", "null"},
		"description": "Audience ID; mutually exclusive with targeting_conditions. Omit or set null for all eligible customers when targeting_conditions is absent.",
	}
	fields := map[string]any{
		"display_name":                 configField("string", "Experiment name"),
		"enrollment_percentage":        map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "description": "Percentage of eligible customers to enroll"},
		"offering_a_id":                configField("string", "Control Offering ID"),
		"offering_b_id":                configField("string", "Treatment Offering ID"),
		"offering_c_id":                configField("string", "Optional third variant Offering ID"),
		"offering_d_id":                configField("string", "Optional fourth variant Offering ID"),
		"audience_id":                  audience,
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
	audience["description"] = "Omit to keep current targeting unless targeting_conditions is supplied. Set null to clear the audience and conditions, or supply targeting_conditions to replace them. A string selects an audience and cannot be combined with targeting_conditions."
	audience["examples"] = []any{"aud_123", nil}
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
				"value":    map[string]any{"type": []string{"string", "integer", "array"}, "items": map[string]any{"type": []string{"string", "integer"}}, "description": "Version string for app_version/sdk_version; array for country/platform/app_config; strings or integers for custom attributes. See field_rules for each field."},
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
