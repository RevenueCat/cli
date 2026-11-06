package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/paywallai"
)

// paywallAIFlowSession is the conversation memory for one Flow, shared by every
// --step-ids scope. The Paywalls AI Editor reads the content from the Flow itself, so no paywall
// is stored.
type paywallAIFlowSession struct {
	SessionID        string            `json:"session_id,omitempty"`
	TraceID          string            `json:"trace_id,omitempty"`
	UIConfig         json.RawMessage   `json:"ui_config"`
	ProductVariables map[string]string `json:"product_variables"`
	SessionItems     json.RawMessage   `json:"__unstable_session_items"`
	AppContext       json.RawMessage   `json:"app_context,omitempty"`
}

// flowPreviewSessionPath keys previews by step: the Paywalls AI Editor renders only the requested
// step when exactly one is named, so that step owns the file. Other runs return
// no step id and keep the flow-wide name.
func flowPreviewSessionPath(sessionPath string, stepIDs []string) string {
	if len(stepIDs) != 1 {
		return sessionPath
	}
	return screenshotBase(sessionPath) + "." + stepIDs[0] + paywallSessionSuffix
}

// flowEditCommand is the follow-up invocation; sessionFlag is the user's own
// --session value, so a default session path never leaks into the hint.
func flowEditCommand(flowID string, stepIDs []string, sessionFlag string) string {
	cmd := "rc flows edit " + flowID
	if len(stepIDs) > 0 {
		cmd += " --step-ids " + strings.Join(stepIDs, ",")
	}
	if sessionFlag != "" {
		cmd += " --session " + sessionFlag
	}
	return cmd
}

func loadFlowSession(path string) (*paywallAIFlowSession, error) {
	payload, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &paywallAIFlowSession{
			UIConfig:         json.RawMessage(minimalUIConfig),
			ProductVariables: map[string]string{},
			SessionItems:     json.RawMessage(`{}`),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading session file: %w", err)
	}
	var session paywallAIFlowSession
	if err := json.Unmarshal(payload, &session); err != nil {
		return nil, fmt.Errorf("parsing session file %s: %w", path, err)
	}
	return &session, nil
}

func applyFlowEvent(session *paywallAIFlowSession, event *paywallai.Event) {
	if event.SessionID != "" {
		session.SessionID = event.SessionID
	}
	if event.TraceID != "" {
		session.TraceID = event.TraceID
	}
	if len(event.SessionItems) > 0 {
		session.SessionItems = event.SessionItems
	}
	if len(event.AppContext) > 0 {
		session.AppContext = event.AppContext
	}
}

func newFlowsEditCmd() *cobra.Command {
	var stepIDs []string
	opts := paywallAIOptions{
		prompt:  os.Getenv("RC_PAYWALL_PROMPT"),
		baseURL: devEnvOrDefault("RC_PAYWALL_AI_BASE_URL", paywallai.DefaultBaseURL),
		timeout: 10 * time.Minute,
	}
	cmd := &cobra.Command{
		Use:   "edit [flow-id] [--step-ids <step-id>[,<step-id>...]]",
		Short: "Edit a Flow screen with the Paywalls AI Editor",
		Long: `Applies a natural-language edit to screen steps of a Flow and saves the
result onto the Flow draft (revision-guarded). The editor sees the whole Flow
but is scoped to --step-ids (comma-separated or repeated); it may also adjust
other screens, which are reported as "also changed". Omit --step-ids to edit
the whole Flow. Find step ids with rc flows show.

Each Flow keeps one editor session in the CLI data dir (--session overrides
the file). --step-ids only scopes a single run, so you can edit some steps,
check them, then continue on other steps (or the whole Flow) and the earlier
instructions are remembered. See rc paywalls edit --help for prompting guidance.`,
		Example: `  rc flows edit wf_abc --step-ids welcome --prompt "Bolder CTA, keep the layout"
  rc flows edit wf_abc --step-ids welcome,pricing --prompt "same button style on both"
  rc flows edit wf_abc --step-ids welcome --prompt "match this" --attachment DESIGN.md --image home.png`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			client, err := rt.API()
			if err != nil {
				return err
			}
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			flowID, err := requireID(rt, argAt(args, 0), "flow", func() ([]PickerItem, error) {
				return flowPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			return runFlowEdit(cmd.Context(), rt, client, projectID, opts, flowID, stepIDs)
		},
	}
	addPaywallAIFlags(cmd, &opts)
	cmd.Flags().StringSliceVar(&stepIDs, "step-ids", nil, "screen steps to edit, comma-separated or repeated (default: the whole flow)")
	return cmd
}

// runFlowEdit drives one Paywalls AI Editor run against screen steps of a Flow and saves
// the edited steps onto the Flow draft itself.
func runFlowEdit(ctx context.Context, rt *Runtime, client *api.Client, projectID string, opts paywallAIOptions, flowID string, stepIDs []string) error {
	draft, err := client.Flows.Get(ctx, projectID, flowID, api.FlowExpandSteps)
	if err != nil {
		if api.IsNotFound(err) {
			return WithHint(err, "Pass the flow id: rc flows list --type paywall")
		}
		return err
	}
	seen := map[string]bool{}
	// Non-nil so no ids serialises as [] (whole flow), never null.
	stepIDs = slices.DeleteFunc(append([]string{}, stepIDs...), func(id string) bool {
		dup := seen[id]
		seen[id] = true
		return dup
	})
	for _, id := range stepIDs {
		if err := requireScreenStep(draft, id); err != nil {
			return err
		}
	}
	if err := requirePaywallAIPrompt(rt, &opts.prompt, "What should change?"); err != nil {
		return err
	}
	editCmd := flowEditCommand(flowID, stepIDs, opts.sessionPath)
	if opts.sessionPath == "" {
		if opts.sessionPath, err = defaultPaywallSessionPath(projectID, flowID); err != nil {
			return err
		}
	}
	session, err := loadFlowSession(opts.sessionPath)
	if err != nil {
		return err
	}
	// Previews from an earlier run must never be mistaken for this run's.
	removePaywallScreenshots(opts.sessionPath)

	editor, err := paywallAIClient(rt, opts.baseURL)
	if err != nil {
		return err
	}
	attachments, textRefs, err := loadPaywallAIAttachments(opts.images, opts.attachments)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, opts.timeout)
	defer cancel()

	rt.Out.Info("Designing with the Paywalls AI Editor — this can take a few minutes…")
	stream, err := editor.StreamV3(ctx, paywallai.EditorRequestV3{
		ProjectID:        projectID,
		SessionID:        session.SessionID,
		UIConfig:         session.UIConfig,
		ProductVariables: session.ProductVariables,
		Message:          withPaywallContext(opts.prompt, opts.context) + textRefs,
		InputAttachments: attachments,
		SessionItems:     session.SessionItems,
		AppContext:       session.AppContext,
		WorkflowContext: paywallai.WorkflowContext{
			Version:           3,
			WorkflowID:        flowID,
			ReferencedStepIDs: stepIDs,
			WorkflowRevision:  draft.Revision,
		},
		IncludeResultScreenshots: true,
	})
	if err != nil {
		return err
	}
	defer stream.Close()

	reportedActivity := 0
	for {
		event, err := stream.Next()
		if err != nil {
			return WithHint(fmt.Errorf("the Paywall AI editor stream ended before the run finished: %w", err), "Nothing was saved. Re-run the command to try again.")
		}
		switch event.Type {
		case paywallai.EventRunStarted, paywallai.EventTurnSnapshot:
			applyFlowEvent(session, event)
			_ = savePaywallAISession(opts.sessionPath, session)
			reportedActivity = reportPaywallAIActivity(rt, event.Activity, reportedActivity)
		case paywallai.EventRunFailed:
			// Nothing is saved on a failed run, even if it carries partial edits.
			if event.SessionID != "" {
				session.SessionID = event.SessionID
				_ = savePaywallAISession(opts.sessionPath, session)
			}
			return WithHint(fmt.Errorf("paywall AI editor run failed (%s): %s", event.Error.Code, event.Error.Message), "Nothing was saved. Re-run the command to try again.")
		case paywallai.EventRunCompleted:
			reportPaywallAIActivity(rt, event.Activity, reportedActivity)
			applyFlowEvent(session, event)
			if err := savePaywallAISession(opts.sessionPath, session); err != nil {
				return err
			}
			return finishFlowEdit(ctx, rt, opts, client, projectID, draft, stepIDs, editCmd, session, event)
		}
	}
}

func finishFlowEdit(ctx context.Context, rt *Runtime, opts paywallAIOptions, client *api.Client, projectID string, draft *api.Flow, stepIDs []string, editCmd string, session *paywallAIFlowSession, event *paywallai.Event) error {
	screenshots := savePaywallScreenshots(rt, flowPreviewSessionPath(opts.sessionPath, stepIDs), event.ResultScreenshots)
	saved, err := saveEditedSteps(ctx, client, projectID, draft, event.EditedPaywallsByStepID)
	if saved == nil {
		saved = []string{}
	}
	alsoChanged := siblingSteps(event.EditedPaywallsByStepID, stepIDs)
	switch {
	case err == nil && len(saved) == 0:
		rt.Out.Info("The editor made no changes to the flow.")
	case err == nil:
		rt.Out.Success(fmt.Sprintf("Design saved to flow draft %s (%s)", draft.ID, strings.Join(saved, ", ")))
		if len(alsoChanged) > 0 {
			rt.Out.Field("Also changed", strings.Join(alsoChanged, ", "), "not in --step-ids")
		}
		if errored := countErroredActivity(event.Activity); errored > 0 {
			rt.Out.Info(fmt.Sprintf("%d editor step(s) errored during the run and were retried by the Paywalls AI Editor — the saved draft is the complete final state (nothing partial is ever saved).", errored))
		}
		// The editor renders the requested step only when exactly one was named.
		previewLabel, darkLabel := "Preview", "Preview (dark)"
		if len(stepIDs) == 1 {
			previewLabel, darkLabel = "Preview ("+stepIDs[0]+")", "Preview ("+stepIDs[0]+", dark)"
		}
		if path, ok := screenshots["light"]; ok {
			rt.Out.Field(previewLabel, path)
		}
		if path, ok := screenshots["dark"]; ok {
			rt.Out.Field(darkLabel, path)
		}
		if len(screenshots) == 0 {
			rt.Out.Info("No preview was returned for the edited steps.")
		}
		rt.Out.Field("View it", flowEditorURL(projectID, draft.ID))
		rt.Out.Field("Keep designing", editCmd)
	default:
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 409 {
			// The edits were made against a stale revision, so they are never replayed automatically.
			rt.Out.Warn("Nothing was saved: the flow changed during the run (dashboard or another client).")
			rt.Out.Hint("Re-run against the current draft:  " + editCmd)
		} else {
			return WithHint(err, "Nothing was saved. Re-run the command to try again.")
		}
	}
	if rt.Out.IsJSON() {
		return rt.Out.Render(map[string]any{
			"flow_id":          draft.ID,
			"dashboard_url":    flowEditorURL(projectID, draft.ID),
			"step_ids":         stepIDs,
			"saved_steps":      saved,
			"also_changed":     alsoChanged,
			"saved_to_draft":   len(saved) > 0,
			"session_id":       session.SessionID,
			"trace_id":         session.TraceID,
			"session_file":     opts.sessionPath,
			"screenshot_paths": screenshots,
			"activity":         event.Activity,
		})
	}
	return nil
}

func flowEditorURL(projectID, flowID string) string {
	return dashboardURL(projectID, "flows", flowID, "editor")
}

// siblingSteps lists edited steps the request did not name; a whole-flow
// request (no ids) has none. Never nil, so it serialises as [].
func siblingSteps(edited map[string]paywallai.PaywallData, requested []string) []string {
	siblings := []string{}
	if len(requested) == 0 {
		return siblings
	}
	for id := range edited {
		if !slices.Contains(requested, id) {
			siblings = append(siblings, id)
		}
	}
	sort.Strings(siblings)
	return siblings
}

// saveEditedSteps writes every step the run edited back in one draft update,
// guarded by the revision the run started from.
func saveEditedSteps(ctx context.Context, client *api.Client, projectID string, draft *api.Flow, edited map[string]paywallai.PaywallData) ([]string, error) {
	if len(edited) == 0 {
		return nil, nil
	}
	updated := map[string]json.RawMessage{}
	for stepID, paywall := range edited {
		if _, ok := draft.Steps[stepID]; !ok {
			return nil, fmt.Errorf("the editor returned step %q, which is not in flow %s", stepID, draft.ID)
		}
		patch, err := editedStepPatch(paywall)
		if err != nil {
			return nil, err
		}
		updated[stepID] = patch
	}
	if _, err := client.Flows.Update(ctx, projectID, draft.ID, api.UpdateFlow{
		Revision:     draft.Revision,
		UpdatedSteps: updated,
	}); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(updated))
	for id := range updated {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// editedStepPatch is the updated_steps entry for an edited screen: only the
// editable screen fields, which the server merges onto the stored step.
func editedStepPatch(paywall paywallai.PaywallData) (json.RawMessage, error) {
	pw := map[string]any{
		"default_locale":           paywall.DefaultLocale,
		"components_config":        paywall.ComponentsConfig,
		"components_localizations": paywall.ComponentsLocalizations,
	}
	if raw := presentJSON(paywall.StateDeclarations); raw != nil {
		pw["state_declarations"] = raw
	}
	if paywall.AutomaticallyScaleFontSize != nil {
		pw["automatically_scale_font_size"] = *paywall.AutomaticallyScaleFontSize
	}
	return json.Marshal(map[string]any{"screen": pw})
}

func requireScreenStep(draft *api.Flow, stepID string) error {
	raw, ok := draft.Steps[stepID]
	if !ok {
		return WithHint(fmt.Errorf("flow %s has no step %q", draft.ID, stepID), "List the flow's steps with: rc flows show "+draft.ID)
	}
	var step struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &step); err != nil {
		return fmt.Errorf("decoding flow step %q: %w", stepID, err)
	}
	if step.Type != "screen" {
		return fmt.Errorf("step %q is a %q step; only screens can be edited", stepID, step.Type)
	}
	return nil
}
