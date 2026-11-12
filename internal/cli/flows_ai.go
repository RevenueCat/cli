package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/paywallai"
)

// paywallAIFlowSession is the conversation memory for one Flow, shared by every
// --step-ids scope. RevenueCat's AI Editor reads the content from the Flow itself, so no paywall
// is stored.
type paywallAIFlowSession struct {
	ProjectID    string          `json:"project_id,omitempty"`
	FlowID       string          `json:"flow_id,omitempty"`
	SessionID    string          `json:"session_id,omitempty"`
	TraceID      string          `json:"trace_id,omitempty"`
	SessionItems json.RawMessage `json:"__unstable_session_items"`
	AppContext   json.RawMessage `json:"app_context,omitempty"`
}

// flowPreviewSessionPath keys previews by step: RevenueCat's AI Editor renders only the requested
// step when exactly one is named, so that step owns the file. Other runs return
// no step id and keep the flow-wide name.
func flowPreviewSessionPath(sessionPath string, stepIDs []string) string {
	if id := singleStepID(stepIDs); id != "" {
		return screenshotBase(sessionPath) + "." + id + paywallSessionSuffix
	}
	return sessionPath
}

// singleStepID is the one step a request names, or "" for none or several.
func singleStepID(stepIDs []string) string {
	if len(stepIDs) == 1 {
		return stepIDs[0]
	}
	return ""
}

// flowEditCommand is the follow-up invocation.
func flowEditCommand(flowID string, stepIDs []string) string {
	cmd := "rc flows edit " + flowID
	if len(stepIDs) > 0 {
		cmd += " --step-ids " + strings.Join(stepIDs, ",")
	}
	return cmd
}

func loadFlowSession(path string) (*paywallAIFlowSession, error) {
	payload, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &paywallAIFlowSession{SessionItems: json.RawMessage(`{}`)}, nil
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
		timeout: aiEditTimeout,
	}
	cmd := &cobra.Command{
		Use:   "edit [flow-id] [--step-ids <step-id>[,<step-id>...]]",
		Short: "Edit Flows and screens from a prompt using RevenueCat's AI Editor",
		Long: `Applies a natural-language edit to screen steps of a Flow and saves the
result onto the Flow draft (revision-guarded). The editor sees the whole Flow
but is scoped to --step-ids (comma-separated or repeated); it may also adjust
other screens, which are reported as "also changed". Omit --step-ids to edit
the whole Flow. Find step ids with rc flows show.

The editor can also generate images for a screen when the prompt asks for it;
image-generation edits can take several minutes.

Each Flow keeps one editor session in the CLI data dir. --step-ids only scopes
a single run, so you can edit some steps, check them, then continue on other
steps (or the whole Flow) and the earlier instructions are remembered. See rc paywalls edit --help for
prompting guidance.`,
		Example: `  rc flows edit wf_abc --step-ids welcome --prompt "Bolder CTA, keep the layout"
  rc flows edit wf_abc --step-ids welcome,pricing --prompt "same button style on both"
  rc flows edit wf_abc --step-ids welcome --prompt "match this" --attachment DESIGN.md --image home.png`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			if argAt(args, 0) == "" && !rt.CanPrompt() {
				return fmt.Errorf("pass a flow ID")
			}
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
	addAIEditorFlags(cmd, &opts)
	cmd.Flags().StringSliceVar(&stepIDs, "step-ids", nil, "screen steps to edit, comma-separated or repeated (default: the whole flow)")
	return cmd
}

// runFlowEdit drives one AI Editor run against screen steps of a Flow and saves
// the edited steps onto the Flow draft itself.
func runFlowEdit(ctx context.Context, rt *Runtime, client *api.Client, projectID string, opts paywallAIOptions, flowID string, stepIDs []string) error {
	draft, err := client.Flows.Get(ctx, projectID, flowID, api.FlowExpandSteps)
	if err != nil {
		if api.IsNotFound(err) {
			return WithHint(err, "Pass the flow id: rc flows list")
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
	editCmd := flowEditCommand(flowID, stepIDs)
	if opts.sessionPath, err = defaultPaywallSessionPath(projectID, flowID); err != nil {
		return err
	}
	session, err := loadFlowSession(opts.sessionPath)
	if err != nil {
		return err
	}
	session.ProjectID, session.FlowID = projectID, flowID
	// Previews from an earlier run must never be mistaken for this run's.
	removePaywallScreenshots(opts.sessionPath, true, nil)

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

	rt.Out.Info("Designing with RevenueCat's AI Editor — this can take a few minutes…")
	stream, err := editor.StreamV3(ctx, paywallai.EditorRequestV3{
		ProjectID:        projectID,
		SessionID:        session.SessionID,
		UIConfig:         json.RawMessage(minimalUIConfig),
		ProductVariables: map[string]string{},
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

	run := &flowEditRun{rt: rt, opts: opts, projectID: projectID, draft: draft, stepIDs: stepIDs, editCmd: editCmd, session: session}
	reportedActivity := 0
	for {
		event, err := stream.Next()
		if err != nil {
			return run.finishUnconfirmed(ctx, client, err)
		}
		switch event.Type {
		case paywallai.EventRunStarted, paywallai.EventTurnSnapshot:
			applyFlowEvent(session, event)
			_ = writeJSONFile(opts.sessionPath, session)
			reportedActivity = reportPaywallAIActivity(rt, event.Activity, reportedActivity)
		case paywallai.EventRunFailed:
			applyFlowEvent(session, event)
			_ = writeJSONFile(opts.sessionPath, session)
			code := event.Error.Code
			switch code {
			case saveStaleCode:
				return run.finishStale(event)
			case saveLockedCode:
				return WithHint(fmt.Errorf("AI Editor run failed (%s): %s", code, event.Error.Message), "Nothing was saved. Wait a moment, then re-run:  "+editCmd)
			case saveRejectedCode:
				return WithHint(fmt.Errorf("the flow rejected the editor's edits: %s", event.Error.Message), "Nothing was saved. Re-run with a different prompt:  "+editCmd)
			}
			return WithHint(fmt.Errorf("AI Editor run failed (%s): %s", code, event.Error.Message), "Nothing was saved. Re-run the command to try again.")
		case paywallai.EventRunCompleted:
			reportPaywallAIActivity(rt, event.Activity, reportedActivity)
			applyFlowEvent(session, event)
			if err := writeJSONFile(opts.sessionPath, session); err != nil {
				rt.Out.Warn(fmt.Sprintf("The edit was saved, but the session file could not be written (%v), so the next run won't include this turn.", err))
			}
			return run.finish(event)
		}
	}
}

// Error codes the editor service reports when saving a finished run fails.
const (
	saveStaleCode    = "save_stale"
	saveLockedCode   = "save_locked"
	saveRejectedCode = "save_rejected"
)

// flowEditRun is the state a finished run reports from.
type flowEditRun struct {
	rt        *Runtime
	opts      paywallAIOptions
	projectID string
	draft     *api.Flow
	stepIDs   []string
	editCmd   string
	session   *paywallAIFlowSession
}

// finishUnconfirmed handles a stream that ended without a final event:
// the service may still have saved, which only the Flow's revision shows.
func (r *flowEditRun) finishUnconfirmed(ctx context.Context, client *api.Client, streamErr error) error {
	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), aiSaveTimeout)
	defer cancel()
	current, err := client.Flows.Get(checkCtx, r.projectID, r.draft.ID)
	switch {
	case err != nil:
		return WithHint(fmt.Errorf("RevenueCat's AI Editor stream ended before the run finished (%v), and the flow could not be re-checked: %w", streamErr, err), "The edit may have been saved. Check the flow before re-running:  rc flows show "+r.draft.ID)
	case current.Revision == r.draft.Revision:
		return WithHint(fmt.Errorf("RevenueCat's AI Editor stream ended before the run finished: %w", streamErr), "The flow is unchanged, so nothing was saved. Re-run the command to try again.")
	}
	r.rt.Out.Warn("The editor stream ended before the final result arrived, but the flow has changed since the run started, so the edit was saved.")
	return r.finish(&paywallai.Event{Revision: &current.Revision})
}

// flowEditOutcome is what both a completed and a stale run report.
type flowEditOutcome struct {
	screenshots map[string]string
	saved       []string
	alsoChanged []string
	editorURL   string
}

func (r *flowEditRun) outcome(event *paywallai.Event) flowEditOutcome {
	saved := event.EditedStepIDs
	if saved == nil {
		saved = []string{}
	}
	return flowEditOutcome{
		screenshots: savePaywallScreenshots(r.rt, flowPreviewSessionPath(r.opts.sessionPath, r.stepIDs), event.ResultScreenshots),
		saved:       saved,
		alsoChanged: siblingSteps(saved, r.stepIDs),
		editorURL:   cmp.Or(event.EditorURL, dashboardURL(r.projectID, "flows", r.draft.ID, "editor")),
	}
}

// finishStale reports edits made against a stale revision, which are never replayed automatically.
func (r *flowEditRun) finishStale(event *paywallai.Event) error {
	out := r.outcome(event)
	r.rt.Out.Warn("The draft changed while this edit ran, so nothing was saved.")
	r.rt.Out.Hint("Re-run to have the editor redo the edit on the latest draft:  " + r.editCmd)
	return r.renderJSON(event, out)
}

// finish reports a completed run.
func (r *flowEditRun) finish(event *paywallai.Event) error {
	out := r.outcome(event)
	if event.Revision == nil {
		r.rt.Out.Info("The editor made no changes to the flow.")
		return r.renderJSON(event, out)
	}
	msg := fmt.Sprintf("Design saved to flow draft %s", r.draft.ID)
	if len(out.saved) > 0 {
		msg += " (" + strings.Join(out.saved, ", ") + ")"
	}
	r.rt.Out.Success(fmt.Sprintf("%s at revision %d", msg, *event.Revision))
	if len(out.alsoChanged) > 0 {
		r.rt.Out.Field("Also changed", strings.Join(out.alsoChanged, ", "), "not in --step-ids")
	}
	if errored := countErroredActivity(event.Activity); errored > 0 {
		r.rt.Out.Info(fmt.Sprintf("%d editor step(s) errored during the run and were retried by RevenueCat's AI Editor — the saved draft is the complete final state (nothing partial is ever saved).", errored))
	}
	// The editor renders the requested step only when exactly one was named.
	previewLabel, darkLabel := "Preview", "Preview (dark)"
	if id := singleStepID(r.stepIDs); id != "" {
		previewLabel, darkLabel = "Preview ("+id+")", "Preview ("+id+", dark)"
	}
	if path, ok := out.screenshots["light"]; ok {
		r.rt.Out.Field(previewLabel, path)
	}
	if path, ok := out.screenshots["dark"]; ok {
		r.rt.Out.Field(darkLabel, path)
	}
	if len(out.screenshots) == 0 {
		r.rt.Out.Info("No preview was returned for the edited steps.")
	}
	r.rt.Out.Field("View it", out.editorURL)
	r.rt.Out.Field("Keep designing", r.editCmd)
	return r.renderJSON(event, out)
}

func (r *flowEditRun) renderJSON(event *paywallai.Event, out flowEditOutcome) error {
	if !r.rt.Out.IsJSON() {
		return nil
	}
	return r.rt.Out.Render(map[string]any{
		"flow_id":          r.draft.ID,
		"dashboard_url":    out.editorURL,
		"step_ids":         r.stepIDs,
		"saved_steps":      out.saved,
		"also_changed":     out.alsoChanged,
		"saved_to_draft":   event.Revision != nil,
		"revision":         event.Revision,
		"session_id":       r.session.SessionID,
		"trace_id":         r.session.TraceID,
		"session_file":     r.opts.sessionPath,
		"screenshot_paths": out.screenshots,
		"activity":         event.Activity,
	})
}

// siblingSteps lists saved steps the request did not name; a whole-flow
// request (no ids) has none. Never nil, so it serialises as [].
func siblingSteps(saved, requested []string) []string {
	siblings := []string{}
	if len(requested) == 0 {
		return siblings
	}
	for _, id := range saved {
		if !slices.Contains(requested, id) {
			siblings = append(siblings, id)
		}
	}
	return siblings
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
