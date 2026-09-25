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
			hintMoreResults(rt, page)
			return nil
		},
	}
	cmd.Flags().StringVar(&state, "state", "", "filter by active, scheduled, or inactive")
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
		Long:  "Creates an inactive Offering rule by default. Without audience_id or conditions, a legacy rule matches everyone. Use --config for audience_id, conditions, schedule, placements, position, or a checkpoint rule with flow_id and checkpoints. Run rc schema targeting create for config fields and accepted values. Active or scheduled rules require confirmation.",
		Example: `  rc targeting create --name "Default paywall" --offering ofrng_default
  echo '{"rule_type":"legacy","display_name":"US paywall","offering_id":"ofrng_us","conditions":[{"field":"country","operator":"in","value":["US"]}]}' | rc targeting create --config - --no-input
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
				showTargetingCreatePlan(rt, body)
				prompt := "Activate targeting rule now?"
				if body.State == "scheduled" {
					prompt = "Schedule targeting rule now?"
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

func showTargetingCreatePlan(rt *Runtime, body api.TargetingRuleCreate) {
	rt.Out.Title("Targeting rule — " + body.DisplayName)
	rt.Out.Lead("Apply this rule in priority order when it becomes active.")
	rt.Out.Field("Type", body.RuleType)
	rt.Out.Field("State", body.State)
	if body.RuleType == "legacy" {
		rt.Out.Field("Offering", body.OfferingID)
		if body.AudienceID != "" {
			rt.Out.Field("Audience", body.AudienceID)
		} else if len(body.Conditions) > 0 && string(body.Conditions) != "[]" {
			rt.Out.Field("Conditions", string(body.Conditions))
		} else {
			rt.Out.Notice("This rule matches everyone. Active rules use the first match.")
		}
		if body.Position != nil {
			rt.Out.Field("Position", fmt.Sprint(*body.Position))
		} else {
			rt.Out.Field("Position", "Append to the end")
		}
		if len(body.Placements) > 0 {
			rt.Out.Field("Placements", string(body.Placements))
		}
	} else {
		rt.Out.Field("Flow", body.FlowID)
		rt.Out.Field("Audience", body.AudienceID)
		rt.Out.Field("Checkpoints", string(body.Checkpoints))
	}
	if len(body.Schedule) > 0 {
		rt.Out.Field("Schedule", string(body.Schedule))
	}
	rt.Out.Plan([]string{"Create the rule and apply it to matching customers"})
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
		Use:     "update [id]",
		Short:   "Update an Offering targeting rule",
		Long:    "Partially updates a legacy targeting rule from a JSON object with position, state, display_name, offering_id, audience_id, conditions, schedule, or placements. Run rc schema targeting update for config fields and accepted values. An active rule or an activation requires confirmation. Checkpoint rule updates are not exposed by this endpoint.",
		Example: `  echo '{"state":"active","position":1}' | rc targeting update trle_123 --config - --yes --no-input`,
		Args:    cobra.MaximumNArgs(1),
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
			if current.RuleType == "checkpoint" {
				return fmt.Errorf("checkpoint rule updates are not supported by this endpoint")
			}
			var newState string
			if value, ok := body["state"]; ok {
				if err := json.Unmarshal(value, &newState); err != nil {
					return fmt.Errorf("state must be active or inactive")
				}
				if newState != "active" && newState != "inactive" {
					return fmt.Errorf("state must be active or inactive")
				}
			}
			if current.State == "active" || newState == "active" {
				rt.Out.Title("Targeting rule — " + current.DisplayName)
				rt.Out.Lead("Change the rule used to choose an Offering for matching customers.")
				rt.Out.Field("Current state", current.State)
				rt.Out.Field("Current offering", current.OfferingID)
				if current.AudienceID != nil {
					rt.Out.Field("Current audience", *current.AudienceID)
				} else if len(current.Conditions) > 0 {
					rt.Out.Field("Current conditions", compactJSON(current.Conditions))
				} else {
					rt.Out.Field("Current audience", "Everyone")
				}
				rt.Out.Field("Changes", compactJSON(body))
				resultingAudience, everyone, err := targetingAudienceAfterUpdate(current, body)
				if err != nil {
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
	allowed := map[string]bool{"position": true, "state": true, "display_name": true, "offering_id": true, "audience_id": true, "conditions": true, "schedule": true, "placements": true}
	for key := range body {
		if !allowed[key] {
			return fmt.Errorf("unknown targeting rule field %q", key)
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
		if err := json.Unmarshal(value, &conditions); err != nil {
			return "", false, fmt.Errorf("conditions must be an array")
		}
	}
	if audienceID != "" {
		return audienceID, false, nil
	}
	if len(conditions) > 0 {
		return compactJSON(conditions), false, nil
	}
	return "Everyone", true, nil
}

func newTargetingDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete [id]",
		Short: "Delete a targeting rule",
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
}
