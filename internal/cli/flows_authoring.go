package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
)

const maxFlowConfigBytes = 5 << 20

func newFlowsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use: "show [id]", Short: "Show a Flow's editable draft", Args: cobra.MaximumNArgs(1),
		Example: `  rc flows show wf_abc
  rc flows show wf_abc --json`,
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
			flowID, err := requireID(rt, argAt(args, 0), "flow", func() ([]PickerItem, error) {
				return flowPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			draft, err := client.Flows.GetDraft(cmd.Context(), projectID, flowID)
			if err != nil {
				return err
			}
			return rt.Out.Render(draft)
		},
	}
}

func newFlowsExportCmd() *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use: "export [id]", Short: "Export an editable Flow update to a JSON file", Args: cobra.MaximumNArgs(1),
		Long: "Writes the current revision, settings, and updated_steps for use with flows update --config. Steps omitted from updated_steps are preserved. Add IDs to deleted_step_ids to remove them.",
		Example: `  rc flows export wf_abc --output flow.json
  rc flows update wf_abc --config flow.json`,
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
			flowID, err := requireID(rt, argAt(args, 0), "flow", func() ([]PickerItem, error) {
				return flowPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			draft, err := client.Flows.GetDraft(cmd.Context(), projectID, flowID)
			if err != nil {
				return err
			}
			body := api.UpdateFlowDraft{FlowSettings: draft.FlowSettings, Revision: draft.Revision, UpdatedSteps: draft.Steps}
			data, err := json.MarshalIndent(body, "", "  ")
			if err != nil {
				return err
			}
			if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
				return err
			}
			return rt.Out.Render(struct {
				Path     string `json:"path"`
				FlowID   string `json:"flow_id"`
				Revision int    `json:"revision"`
			}{path, draft.ID, draft.Revision})
		},
	}
	cmd.Flags().StringVar(&path, "output", "", "write the editable update to this file")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}

func newFlowsUpdateCmd() *cobra.Command {
	var config string
	cmd := &cobra.Command{
		Use: "update [id]", Short: "Save changes to a Flow draft", Args: cobra.MaximumNArgs(1),
		Long: "Saves settings and step changes atomically. The config must include the revision from flows export or flows show. Omitted steps are preserved; deleted_step_ids removes steps explicitly. A stale revision fails without saving. Does not publish.",
		Example: `  rc flows update wf_abc --config flow.json
  cat flow.json | rc flows update wf_abc --config -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var body api.UpdateFlowDraft
			if err := readJSONConfigWithLimit(config, &body, maxFlowConfigBytes); err != nil {
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
			flowID, err := requireID(rt, argAt(args, 0), "flow", func() ([]PickerItem, error) {
				return flowPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			flow, err := client.Flows.UpdateDraft(cmd.Context(), projectID, flowID, body)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Saved draft of %s", flow.ID))
			return rt.Out.Render(flow)
		},
	}
	cmd.Flags().StringVar(&config, "config", "", "JSON update file, or - for stdin (up to 5 MiB)")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

func newFlowsCreateCmd() *cobra.Command {
	var config string
	cmd := &cobra.Command{
		Use: "create", Short: "Create an unpublished Flow from JSON", Args: cobra.NoArgs,
		Long: "Creates a draft with display_name, type (paywall, web-funnel, or general), optional settings, and steps keyed by step ID. Type defaults to general. Does not publish or attach a checkpoint rule.",
		Example: `  rc flows create --config flow.json
  cat flow.json | rc flows create --config -`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var body api.CreateFlow
			if err := readJSONConfigWithLimit(config, &body, maxFlowConfigBytes); err != nil {
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
			flow, err := client.Flows.Create(cmd.Context(), projectID, body)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Created %s", flow.ID))
			return rt.Out.Render(flow)
		},
	}
	cmd.Flags().StringVar(&config, "config", "", "JSON flow file, or - for stdin (up to 5 MiB)")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

func newFlowsDuplicateCmd() *cobra.Command {
	var name, sourceVersion string
	cmd := &cobra.Command{
		Use: "duplicate [id]", Short: "Duplicate a Flow into an unpublished draft", Args: cobra.MaximumNArgs(1),
		Example: `  rc flows duplicate wf_abc --name "Onboarding copy"
  rc flows duplicate wf_abc --source-version published`,
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
			flowID, err := requireID(rt, argAt(args, 0), "flow", func() ([]PickerItem, error) {
				return flowPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			source := api.DuplicateFlowSourceVersion(sourceVersion)
			body := api.DuplicateFlow{SourceVersion: &source}
			if name != "" {
				body.DisplayName = &name
			}
			flow, err := client.Flows.Duplicate(cmd.Context(), projectID, flowID, body)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Duplicated %s as %s", flowID, flow.ID))
			return rt.Out.Render(flow)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "name of the duplicate")
	cmd.Flags().StringVar(&sourceVersion, "source-version", string(api.DuplicateFlowSourceVersionDraft), "source to copy: draft or published")
	return cmd
}

func newFlowsValidateCmd() *cobra.Command {
	var revision int
	cmd := &cobra.Command{
		Use: "validate [id]", Short: "Validate a saved Flow draft", Args: cobra.MaximumNArgs(1),
		Long: "Returns workflow errors and step issues for the saved draft. Returns a nonzero exit status when the draft is invalid. Does not save or publish.",
		Example: `  rc flows validate wf_abc
  rc flows validate wf_abc --revision 4`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("revision") && revision < 1 {
				return fmt.Errorf("--revision must be positive")
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
			flowID, err := requireID(rt, argAt(args, 0), "flow", func() ([]PickerItem, error) {
				return flowPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			validation, err := client.Flows.Validate(cmd.Context(), projectID, flowID, revision)
			if err != nil {
				return err
			}
			if err := rt.Out.Render(validation); err != nil {
				return err
			}
			if !validation.Valid {
				return &SilentExitError{Code: 1}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&revision, "revision", 0, "require this draft revision")
	return cmd
}

func newFlowsPublishCmd() *cobra.Command {
	var revision int
	cmd := &cobra.Command{
		Use: "publish [id]", Short: "Validate and publish a specific Flow draft revision", Args: cobra.MaximumNArgs(1),
		Long: "Publishes the specified draft revision, including its screens. This can change flows served through offerings, checkpoint rules, and web links. A stale revision or invalid draft is rejected. Confirmation: prompts under TTY; --yes is required under --no-input.",
		Example: `  rc flows publish wf_abc --revision 4
  rc flows publish wf_abc --revision 4 --yes --no-input --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if revision < 1 {
				return fmt.Errorf("--revision must be positive")
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
			flowID, err := requireID(rt, argAt(args, 0), "flow", func() ([]PickerItem, error) {
				return flowPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			if err := confirmOrAbort(rt, fmt.Sprintf("Publish flow %q at revision %d?", flowID, revision)); err != nil {
				return err
			}
			flow, err := client.Flows.Publish(cmd.Context(), projectID, flowID, revision)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Published %s", flow.ID))
			return rt.Out.Render(flow)
		},
	}
	cmd.Flags().IntVar(&revision, "revision", 0, "draft revision to publish")
	_ = cmd.MarkFlagRequired("revision")
	return cmd
}
