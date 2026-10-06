package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

func newTargetingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "targeting",
		Short: "Manage targeting rules for Offerings and Flows",
		Long:  "List, inspect, and manage targeting rules. Active rules are evaluated in priority order; the first match wins.",
	}
	cmd.AddCommand(newTargetingListCmd(), newTargetingShowCmd(), newTargetingCreateCmd(), newTargetingUpdateCmd(), newTargetingDeleteCmd())
	return cmd
}

func newTargetingListCmd() *cobra.Command {
	var state, cursor string
	var limit int
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List targeting rules",
		Long:    "Returns one page in API order. Active Offering rules are listed in evaluation order; the first match wins. The API does not return absolute rule positions. Use --state active and --cursor to inspect subsequent pages before reordering.",
		Example: "  rc targeting list --state active\n  rc targeting list --json",
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
			page, err := client.TargetingRules.List(cmd.Context(), projectID, api.ListTargetingRulesOptions{State: state, Limit: limit, StartingAfter: cursor})
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(page.Items))
			for _, rule := range page.Items {
				serves := rule.OfferingID
				if rule.RuleType == "checkpoint" {
					serves = rule.FlowID
				}
				rows = append(rows, []string{rule.ID, rule.DisplayName, rule.RuleType, rule.State, serves})
			}
			if err := rt.Out.RenderTable(output.Table{Columns: []string{"ID", "NAME", "TYPE", "STATE", "SERVES"}, Rows: rows, Raw: page}); err != nil {
				return err
			}
			if state == "" || state == "active" {
				rt.Out.Info("Active Offering rules are listed in evaluation order; the first match wins.")
			}
			hintMoreResults(rt, page)
			return nil
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "filter by active, scheduled (checkpoint only), or inactive; future-scheduled Offering rules are active")
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum rules to return (1–100)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "item ID to start after (pagination)")
	return cmd
}

func targetingPickerItems(cmd *cobra.Command, client *api.Client, projectID string) ([]PickerItem, error) {
	page, err := client.TargetingRules.List(cmd.Context(), projectID, api.ListTargetingRulesOptions{})
	if err != nil {
		return nil, err
	}
	items := make([]PickerItem, len(page.Items))
	for i, rule := range page.Items {
		items[i] = PickerItem{ID: rule.ID, Label: fmt.Sprintf("%s  (%s)", rule.DisplayName, rule.State)}
	}
	return items, nil
}

func newTargetingShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "show [id]",
		Short:   "Show a targeting rule",
		Example: "  rc targeting show trle123 --json",
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
			id, err := requireID(rt, argAt(args, 0), "targeting rule", func() ([]PickerItem, error) {
				return targetingPickerItems(cmd, client, projectID)
			})
			if err != nil {
				return err
			}
			rule, err := client.TargetingRules.Get(cmd.Context(), projectID, id)
			if err != nil {
				return err
			}
			return renderTargetingShow(rt, rule)
		},
	}
}

func newTargetingCreateCmd() *cobra.Command {
	var name, offering, state, config string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a targeting rule",
		Long:  "Creates an inactive Offering rule by default. Without audience_id or conditions, a legacy rule matches everyone. Use --config for audience_id, conditions, schedule, placements, position, or a checkpoint rule with flow_id and checkpoints. To schedule an Offering rule, set state to active and schedule.start_date to a future UTC time; it will not match customers before then. The scheduled state is only for checkpoint rules. Run rc schema targeting create for config fields and accepted values. Active or scheduled rules require confirmation.",
		Example: `  rc targeting create --name "Default paywall" --offering ofrng_default
  echo '{"rule_type":"legacy","display_name":"US paywall","offering_id":"ofrng_us","conditions":[{"field":"country","operator":"in","value":["US"]}]}' | rc targeting create --config - --no-input
  echo '{"display_name":"Holiday paywall","offering_id":"ofrng_holiday","state":"active","schedule":{"start_date":"2030-12-01T00:00:00Z","end_date":"2030-12-31T23:59:59Z"}}' | rc targeting create --config - --yes --no-input
  echo '{"rule_type":"checkpoint","display_name":"After onboarding","audience_id":"aud_123","flow_id":"wf_123","checkpoints":[{"checkpoint_id":"chkpt_123"}]}' | rc targeting create --config - --no-input`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			body := api.TargetingRuleCreate{}
			if config != "" {
				if err := readJSONConfig(config, &body); err != nil {
					return err
				}
			}
			if cmd.Flags().Changed("name") {
				body.DisplayName = name
			}
			if cmd.Flags().Changed("offering") {
				body.OfferingID = offering
			}
			if cmd.Flags().Changed("state") {
				body.State = state
			}
			if body.RuleType == "" {
				body.RuleType = "legacy"
			}
			if body.State == "" {
				body.State = "inactive"
			}
			if err := gatherTargetingCreateInput(cmd, rt, projectID, &body); err != nil {
				return err
			}
			if err := validateTargetingCreate(body); err != nil {
				return err
			}
			if body.State != "inactive" {
				if err := showTargetingCreatePlan(rt, body); err != nil {
					return err
				}
				prompt := "Activate targeting rule now?"
				if body.State == "scheduled" {
					prompt = "Schedule targeting rule now?"
				} else if len(body.Schedule) > 0 {
					prompt = "Create targeting rule with schedule now?"
				}
				if err := confirmOrAbort(rt, prompt); err != nil {
					return err
				}
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			rule, err := client.TargetingRules.Create(cmd.Context(), projectID, body)
			if err != nil {
				return err
			}
			rt.Out.Success("Created targeting rule " + rule.ID)
			return renderTargetingShow(rt, rule)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "rule display name")
	cmd.Flags().StringVar(&offering, "offering", "", "Offering ID served by a legacy rule")
	cmd.Flags().StringVar(&state, "state", "", "inactive (default), active, or scheduled (checkpoint only)")
	cmd.Flags().StringVar(&config, "config", "", "JSON config file; use - for stdin")
	return cmd
}

func gatherTargetingCreateInput(cmd *cobra.Command, rt *Runtime, projectID string, body *api.TargetingRuleCreate) error {
	if body.RuleType == "checkpoint" {
		var missing []string
		if body.DisplayName == "" {
			missing = append(missing, "display_name")
		}
		if body.AudienceID == "" {
			missing = append(missing, "audience_id")
		}
		if body.FlowID == "" {
			missing = append(missing, "flow_id")
		}
		if len(body.Checkpoints) == 0 {
			missing = append(missing, "checkpoints")
		}
		if len(missing) > 0 {
			return fmt.Errorf("checkpoint rule requires %s in --config", strings.Join(missing, ", "))
		}
		return nil
	}
	if !rt.CanPrompt() {
		var missing []string
		if body.DisplayName == "" {
			missing = append(missing, "--name")
		}
		if body.OfferingID == "" {
			missing = append(missing, "--offering")
		}
		if len(missing) > 0 {
			return fmt.Errorf("targeting input required: pass %s or --config <file>", strings.Join(missing, ", "))
		}
		return nil
	}
	form := tui.Form(false)
	if body.DisplayName == "" {
		form.Field(huh.NewInput().Title("Rule name").Value(&body.DisplayName).Validate(tui.Required("rule name")))
	}
	if err := form.Run(); err != nil {
		return err
	}
	if body.OfferingID == "" {
		client, err := rt.API()
		if err != nil {
			return err
		}
		body.OfferingID, err = requireID(rt, "", "offering", func() ([]PickerItem, error) {
			return offeringPickerItems(cmd.Context(), client, projectID)
		})
		return err
	}
	return nil
}

func showTargetingCreatePlan(rt *Runtime, body api.TargetingRuleCreate) error {
	rt.Out.Title("Targeting rule — " + body.DisplayName)
	if len(body.Schedule) > 0 {
		rt.Out.Lead("Match customers during the scheduled window, in priority order.")
	} else {
		rt.Out.Lead("Apply this rule in priority order when it becomes active.")
	}
	rt.Out.Field("Type", body.RuleType)
	rt.Out.Field("State", body.State)
	if body.RuleType == "legacy" {
		rt.Out.Field("Offering", body.OfferingID)
		if body.AudienceID != "" {
			rt.Out.Field("Audience", body.AudienceID)
		} else if len(body.Conditions) > 0 && string(body.Conditions) != "[]" {
			conditions, err := targetingConfigSummary("conditions", body.Conditions)
			if err != nil {
				return err
			}
			rt.Out.Field("Conditions", conditions)
		} else {
			if len(body.Schedule) > 0 {
				rt.Out.Notice("During its scheduled window, this rule matches everyone. Active rules use the first match.")
			} else {
				rt.Out.Notice("This rule matches everyone. Active rules use the first match.")
			}
		}
		if body.Position != nil {
			rt.Out.Field("Position", fmt.Sprint(*body.Position))
		} else {
			rt.Out.Field("Position", "Append to the end")
		}
		if len(body.Placements) > 0 {
			placements, err := targetingConfigSummary("placements", body.Placements)
			if err != nil {
				return err
			}
			rt.Out.Field("Placements", placements)
		}
	} else {
		rt.Out.Field("Flow", body.FlowID)
		rt.Out.Field("Audience", body.AudienceID)
		checkpoints, err := targetingConfigSummary("checkpoints", body.Checkpoints)
		if err != nil {
			return err
		}
		rt.Out.Field("Checkpoints", checkpoints)
	}
	if len(body.Schedule) > 0 {
		schedule, err := targetingConfigSummary("schedule", body.Schedule)
		if err != nil {
			return err
		}
		rt.Out.Field("Schedule", schedule)
	}
	step := "Create the rule and apply it to matching customers"
	if len(body.Schedule) > 0 {
		step = "Create the rule and match customers during its scheduled window"
	}
	rt.Out.Plan([]string{step})
	return nil
}

func validateTargetingCreate(body api.TargetingRuleCreate) error {
	if body.RuleType != "legacy" && body.RuleType != "checkpoint" {
		return fmt.Errorf("rule_type must be legacy or checkpoint")
	}
	if body.State != "inactive" && body.State != "active" && !(body.RuleType == "checkpoint" && body.State == "scheduled") {
		return fmt.Errorf("state must be inactive or active (checkpoint rules also support scheduled)")
	}
	if body.RuleType == "legacy" && body.AudienceID != "" && len(body.Conditions) > 0 && string(body.Conditions) != "[]" {
		return fmt.Errorf("audience_id and conditions cannot both be set")
	}
	return nil
}

func newTargetingUpdateCmd() *cobra.Command {
	var config string
	cmd := &cobra.Command{
		Use:   "update [id]",
		Short: "Update a targeting rule",
		Long:  "Partially updates a targeting rule. Offering rules accept position, state, display_name, offering_id, audience_id, conditions, schedule, or placements. To schedule an Offering rule, set state to active and schedule.start_date to a future UTC time; it will not match customers before then. Run rc schema targeting update for config fields and accepted values. Active or scheduled rules, and transitions into those states, require confirmation. Checkpoint rules accept state, display_name, audience_id, flow_id, checkpoints, and schedule.",
		Example: `  echo '{"state":"active","position":1}' | rc targeting update trle_123 --config - --yes --no-input
  echo '{"state":"active","schedule":{"start_date":"2030-12-01T00:00:00Z","end_date":"2030-12-31T23:59:59Z"}}' | rc targeting update trle_123 --config - --yes --no-input
  echo '{"flow_id":"wf_new","checkpoints":[{"checkpoint_id":"chkpt_new"}]}' | rc targeting update chkptrule_123 --config - --yes --no-input`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if config == "" {
				return fmt.Errorf("pass --config <file> or --config - for stdin")
			}
			body := api.TargetingRuleUpdate{}
			if err := readJSONConfig(config, &body); err != nil {
				return err
			}
			if err := validTargetingUpdate(body); err != nil {
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
			id, err := requireID(rt, argAt(args, 0), "targeting rule", func() ([]PickerItem, error) {
				return targetingPickerItems(cmd, client, projectID)
			})
			if err != nil {
				return err
			}
			current, err := client.TargetingRules.Get(cmd.Context(), projectID, id)
			if err != nil {
				return err
			}
			if err := validTargetingUpdateForRule(current.RuleType, body); err != nil {
				return err
			}
			var newState string
			if value, ok := body["state"]; ok {
				if err := json.Unmarshal(value, &newState); err != nil || (newState != "active" && newState != "inactive" && !(current.RuleType == "checkpoint" && newState == "scheduled")) {
					return fmt.Errorf("state must be active or inactive (checkpoint rules also support scheduled)")
				}
			}
			resultingAudience, everyone, err := targetingAudienceAfterUpdate(current, body)
			if err != nil {
				return err
			}
			if current.State != "inactive" || newState == "active" || newState == "scheduled" {
				rt.Out.Title("Targeting rule — " + current.DisplayName)
				if current.RuleType == "checkpoint" {
					rt.Out.Lead("Change the rule used to choose a Flow at a checkpoint.")
					rt.Out.Field("Current flow", current.FlowID)
				} else {
					rt.Out.Lead("Change the rule used to choose an Offering for matching customers.")
					rt.Out.Field("Current offering", current.OfferingID)
				}
				rt.Out.Field("ID", current.ID)
				rt.Out.Field("Current state", current.State)
				if current.AudienceID != nil {
					rt.Out.Field("Current audience", *current.AudienceID)
				} else if len(current.Conditions) > 0 {
					conditions, err := targetingConditionsSummary(current.Conditions)
					if err != nil {
						return err
					}
					rt.Out.Field("Current conditions", conditions)
				} else {
					rt.Out.Field("Current audience", "Everyone")
				}
				if err := showTargetingUpdateChanges(rt, body); err != nil {
					return err
				}
				rt.Out.Field("Resulting audience", resultingAudience)
				if everyone {
					rt.Out.Notice("Resulting rule matches everyone. Active rules use the first match.")
				}
				rt.Out.Plan([]string{"Update the targeting rule"})
				if err := confirmOrAbort(rt, "Update targeting rule now?"); err != nil {
					return err
				}
			}
			rule, err := client.TargetingRules.Update(cmd.Context(), projectID, id, body)
			if err != nil {
				return err
			}
			rt.Out.Success("Updated targeting rule " + rule.ID)
			return renderTargetingShow(rt, rule)
		},
	}
	cmd.Flags().StringVar(&config, "config", "", "JSON object of fields to update; use - for stdin")
	return cmd
}

func validTargetingUpdate(body api.TargetingRuleUpdate) error {
	if len(body) == 0 {
		return fmt.Errorf("config must contain at least one field")
	}
	allowed := map[string]bool{"position": true, "state": true, "display_name": true, "offering_id": true, "audience_id": true, "conditions": true, "schedule": true, "placements": true, "flow_id": true, "checkpoints": true}
	for key := range body {
		if !allowed[key] {
			return fmt.Errorf("unknown targeting rule field %q", key)
		}
	}
	return nil
}

func validTargetingUpdateForRule(ruleType string, body api.TargetingRuleUpdate) error {
	if ruleType != "checkpoint" {
		for _, field := range []string{"flow_id", "checkpoints"} {
			if _, ok := body[field]; ok {
				return fmt.Errorf("%s can only be set on checkpoint rules", field)
			}
		}
		return nil
	}
	for _, field := range []string{"position", "offering_id", "conditions", "placements"} {
		if _, ok := body[field]; ok {
			return fmt.Errorf("%s cannot be set on checkpoint rules", field)
		}
	}
	if value, ok := body["audience_id"]; ok {
		var audience string
		if err := json.Unmarshal(value, &audience); err != nil || audience == "" {
			return fmt.Errorf("checkpoint audience_id must be a nonempty string")
		}
	}
	return nil
}

func targetingAudienceAfterUpdate(current *api.TargetingRule, body api.TargetingRuleUpdate) (string, bool, error) {
	audienceID := ""
	if current.AudienceID != nil {
		audienceID = *current.AudienceID
	}
	conditions := current.Conditions
	if value, ok := body["audience_id"]; ok {
		audienceID = ""
		if err := json.Unmarshal(value, &audienceID); err != nil {
			return "", false, fmt.Errorf("audience_id must be a string or null")
		}
	}
	if value, ok := body["conditions"]; ok {
		conditions = nil
		if err := json.Unmarshal(value, &conditions); err != nil {
			return "", false, fmt.Errorf("conditions must be an array")
		}
	}
	if audienceID != "" && len(conditions) > 0 {
		return "", false, fmt.Errorf("audience_id and conditions cannot both be set; clear the other field in the same update")
	}
	if audienceID != "" {
		return audienceID, false, nil
	}
	if len(conditions) > 0 {
		formatted, err := targetingConditionsSummary(conditions)
		return formatted, false, err
	}
	return "Everyone", true, nil
}

func newTargetingDeleteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete [id]",
		Short: "Delete a targeting rule",
		Long:  "Permanently deletes a targeting rule. Active or scheduled rules require --force in addition to confirmation or --yes because deleting them may change which Offering customers see.",
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
			id, err := requireID(rt, argAt(args, 0), "targeting rule", func() ([]PickerItem, error) {
				return targetingPickerItems(cmd, client, projectID)
			})
			if err != nil {
				return err
			}
			current, err := client.TargetingRules.Get(cmd.Context(), projectID, id)
			if err != nil {
				return err
			}
			if current.State != "inactive" && !force {
				return WithHint(
					fmt.Errorf("targeting rule %s is %s and may be serving customers; pass --force to delete it", id, current.State),
					"Deactivate it first, or pass --force after confirming this rule should be deleted.",
				)
			}
			if current.State != "inactive" {
				rt.Out.Warn("Deleting this rule may change which Offering customers see.")
			}
			if err := confirmOrAbort(rt, "Delete targeting rule "+id+"?"); err != nil {
				return err
			}
			if err := client.TargetingRules.Delete(cmd.Context(), projectID, id); err != nil {
				return err
			}
			rt.Out.Success("Deleted targeting rule " + id)
			return rt.Out.Render(map[string]any{"id": id, "deleted": true})
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "delete an active or scheduled targeting rule")
	return cmd
}
