package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
)

const maxFlowConfigBytes = 5 << 20

func flowClient(cmd *cobra.Command) (*Runtime, *api.Client, string, error) {
	rt := RuntimeFrom(cmd.Context())
	projectID, err := requireProject(rt)
	if err != nil {
		return nil, nil, "", err
	}
	client, err := rt.API()
	return rt, client, projectID, err
}

func newFlowsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use: "show <id>", Short: "Show a Flow's editable draft", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, client, projectID, err := flowClient(cmd)
			if err != nil {
				return err
			}
			draft, err := client.Flows.GetDraft(cmd.Context(), projectID, args[0])
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
		Use: "export <id>", Short: "Export an editable Flow update to a JSON file", Args: cobra.ExactArgs(1),
		Long: "Writes the current revision, settings, and updated_steps for use with flows update --config. Steps omitted from updated_steps are preserved. Add IDs to deleted_step_ids to remove them. Refuses to overwrite an existing file.",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, client, projectID, err := flowClient(cmd)
			if err != nil {
				return err
			}
			draft, err := client.Flows.GetDraft(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			body := api.UpdateFlowDraft{FlowSettings: draft.FlowSettings, Revision: draft.Revision, UpdatedSteps: draft.Steps}
			data, err := json.MarshalIndent(body, "", "  ")
			if err != nil {
				return err
			}
			file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return err
			}
			if _, err := file.Write(append(data, '\n')); err != nil {
				_ = file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
			return rt.Out.Render(struct {
				Path     string `json:"path"`
				FlowID   string `json:"flow_id"`
				Revision int    `json:"revision"`
			}{path, draft.ID, draft.Revision})
		},
	}
	cmd.Flags().StringVar(&path, "output", "", "write the editable update to this new file")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}

func newFlowsUpdateCmd() *cobra.Command {
	var config string
	cmd := &cobra.Command{
		Use: "update <id>", Short: "Save changes to a Flow draft", Args: cobra.ExactArgs(1),
		Long: "Saves settings and step changes atomically. The config must include the revision from flows export or flows show. Omitted steps are preserved; deleted_step_ids removes steps explicitly. A stale revision fails without saving. Does not publish.",
		RunE: func(cmd *cobra.Command, args []string) error {
			var body api.UpdateFlowDraft
			if err := readJSONConfigWithLimit(config, &body, maxFlowConfigBytes); err != nil {
				return err
			}
			if body.Revision < 1 {
				return fmt.Errorf("config must include a positive revision from the current draft")
			}
			rt, client, projectID, err := flowClient(cmd)
			if err != nil {
				return err
			}
			flow, err := client.Flows.UpdateDraft(cmd.Context(), projectID, args[0], body)
			if err != nil {
				return err
			}
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			var body api.CreateFlow
			if err := readJSONConfigWithLimit(config, &body, maxFlowConfigBytes); err != nil {
				return err
			}
			if body.DisplayName == nil || *body.DisplayName == "" {
				return fmt.Errorf("config must include display_name")
			}
			rt, client, projectID, err := flowClient(cmd)
			if err != nil {
				return err
			}
			flow, err := client.Flows.Create(cmd.Context(), projectID, body)
			if err != nil {
				return err
			}
			return rt.Out.Render(flow)
		},
	}
	cmd.Flags().StringVar(&config, "config", "", "JSON flow file, or - for stdin (up to 5 MiB)")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

func newFlowsDuplicateCmd() *cobra.Command {
	body := api.DuplicateFlow{}
	cmd := &cobra.Command{
		Use: "duplicate <id>", Short: "Duplicate a Flow into an unpublished draft", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if body.SourceVersion != "draft" && body.SourceVersion != "published" {
				return fmt.Errorf("--source-version must be draft or published")
			}
			rt, client, projectID, err := flowClient(cmd)
			if err != nil {
				return err
			}
			flow, err := client.Flows.Duplicate(cmd.Context(), projectID, args[0], body)
			if err != nil {
				return err
			}
			return rt.Out.Render(flow)
		},
	}
	cmd.Flags().StringVar(&body.DisplayName, "name", "", "name of the duplicate")
	cmd.Flags().StringVar(&body.SourceVersion, "source-version", "draft", "source to copy: draft or published")
	return cmd
}

func newFlowsValidateCmd() *cobra.Command {
	var revision int
	cmd := &cobra.Command{
		Use: "validate <id>", Short: "Validate a saved Flow draft", Args: cobra.ExactArgs(1),
		Long: "Returns workflow errors and step issues for the saved draft. Returns a nonzero exit status when the draft is invalid. Does not save or publish.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("revision") && revision < 1 {
				return fmt.Errorf("--revision must be positive")
			}
			rt, client, projectID, err := flowClient(cmd)
			if err != nil {
				return err
			}
			validation, err := client.Flows.Validate(cmd.Context(), projectID, args[0], revision)
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
		Use: "publish <id>", Short: "Validate and publish a specific Flow draft revision", Args: cobra.ExactArgs(1),
		Long: "Publishes the specified draft revision, including its screens. This can change flows served through offerings, checkpoint rules, and web links. A stale revision or invalid draft is rejected. Confirmation: prompts under TTY; --yes is required under --no-input.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if revision < 1 {
				return fmt.Errorf("--revision must be positive")
			}
			rt, client, projectID, err := flowClient(cmd)
			if err != nil {
				return err
			}
			if err := confirmOrAbort(rt, fmt.Sprintf("Publish flow %q at revision %d?", args[0], revision)); err != nil {
				return err
			}
			flow, err := client.Flows.Publish(cmd.Context(), projectID, args[0], revision)
			if err != nil {
				return err
			}
			return rt.Out.Render(flow)
		},
	}
	cmd.Flags().IntVar(&revision, "revision", 0, "draft revision to publish")
	_ = cmd.MarkFlagRequired("revision")
	return cmd
}
