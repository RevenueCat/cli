package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/paywallai"
)

// paywallAIFlowSession is the conversation memory for one Flow step. Astra
// reads the step's content from the Flow itself, so no paywall is stored.
type paywallAIFlowSession struct {
	Version          int               `json:"version"`
	ProjectID        string            `json:"project_id"`
	FlowID           string            `json:"flow_id"`
	StepID           string            `json:"step_id"`
	SessionID        string            `json:"session_id,omitempty"`
	TraceID          string            `json:"trace_id,omitempty"`
	UIConfig         json.RawMessage   `json:"ui_config"`
	ProductVariables map[string]string `json:"product_variables"`
	SessionItems     json.RawMessage   `json:"__unstable_session_items"`
	AppContext       json.RawMessage   `json:"app_context,omitempty"`
}

func defaultFlowSessionPath(projectID, flowID, stepID string) (string, error) {
	base, err := defaultPaywallSessionPath(projectID, flowID)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(base), "session."+stepID+paywallSessionSuffix), nil
}

func loadFlowSession(path, projectID, flowID, stepID string) *paywallAIFlowSession {
	fresh := &paywallAIFlowSession{
		Version:          2,
		ProjectID:        projectID,
		FlowID:           flowID,
		StepID:           stepID,
		UIConfig:         json.RawMessage(minimalUIConfig),
		ProductVariables: map[string]string{},
		SessionItems:     json.RawMessage(`{}`),
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return fresh
	}
	var stored paywallAIFlowSession
	if json.Unmarshal(payload, &stored) != nil || stored.Version != 2 ||
		stored.ProjectID != projectID || stored.FlowID != flowID || stored.StepID != stepID {
		return fresh
	}
	return &stored
}

func saveFlowSession(path string, session *paywallAIFlowSession) error {
	payload, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(payload, '\n'), 0o600)
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

// runFlowEdit drives one Astra run against a screen step of a Flow and saves
// the edited steps onto the Flow draft itself.
func runFlowEdit(ctx context.Context, rt *Runtime, opts paywallAIOptions, flowID, stepID string) error {
	client, err := rt.API()
	if err != nil {
		return err
	}
	projectID, err := requireProject(rt)
	if err != nil {
		return err
	}
	draft, err := client.Flows.GetDraft(ctx, projectID, flowID)
	if err != nil {
		if api.IsNotFound(err) {
			return WithHint(err, "Pass the flow id: rc flows list --type paywall")
		}
		return err
	}
	if err := requireScreenStep(draft, stepID); err != nil {
		return err
	}
	if err := requirePaywallAIPrompt(rt, &opts.prompt, "What should change?"); err != nil {
		return err
	}
	if opts.sessionPath, err = defaultFlowSessionPath(projectID, flowID, stepID); err != nil {
		return err
	}
	session := loadFlowSession(opts.sessionPath, projectID, flowID, stepID)

	astra, err := paywallAIClient(rt, opts.baseURL)
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
	stream, err := astra.StreamV3(ctx, paywallai.EditorRequestV3{
		ProjectID:        projectID,
		SessionID:        session.SessionID,
		UIConfig:         session.UIConfig,
		ProductVariables: session.ProductVariables,
		Message:          withPaywallContext(opts.prompt, opts.context) + textRefs,
		InputAttachments: attachments,
		SessionItems:     session.SessionItems,
		AppContext:       session.AppContext,
		WorkflowContext: paywallai.WorkflowContext{
			Version:          3,
			WorkflowID:       flowID,
			FocusedStepID:    stepID,
			WorkflowRevision: draft.Revision,
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
			_ = saveFlowSession(opts.sessionPath, session)
			reportedActivity = reportPaywallAIActivity(rt, event.Activity, reportedActivity)
		case paywallai.EventRunFailed:
			// Nothing is saved on a failed run, even if it carries partial edits.
			if event.SessionID != "" {
				session.SessionID = event.SessionID
				_ = saveFlowSession(opts.sessionPath, session)
			}
			return WithHint(fmt.Errorf("paywall AI editor run failed (%s): %s", event.Error.Code, event.Error.Message), "Nothing was saved. Re-run the command to try again.")
		case paywallai.EventRunCompleted:
			reportPaywallAIActivity(rt, event.Activity, reportedActivity)
			applyFlowEvent(session, event)
			if err := saveFlowSession(opts.sessionPath, session); err != nil {
				return err
			}
			return finishFlowEdit(ctx, rt, opts, client, draft, session, event)
		}
	}
}

func finishFlowEdit(ctx context.Context, rt *Runtime, opts paywallAIOptions, client *api.Client, draft *api.FlowDraft, session *paywallAIFlowSession, event *paywallai.Event) error {
	screenshots := savePaywallScreenshots(rt, opts.sessionPath, event.ResultScreenshots)
	saved, err := saveEditedSteps(ctx, client, session.ProjectID, draft, event.EditedPaywallsByStepID)
	switch {
	case err == nil && len(saved) == 0:
		rt.Out.Info("The editor made no changes to the flow.")
	case err == nil:
		rt.Out.Success(fmt.Sprintf("Design saved to flow draft %s (%s)", session.FlowID, strings.Join(saved, ", ")))
		if path, ok := screenshots["light"]; ok {
			rt.Out.Field("Preview", path)
		}
		if path, ok := screenshots["dark"]; ok {
			rt.Out.Field("Preview (dark)", path)
		}
		rt.Out.Field("Keep designing", fmt.Sprintf("rc paywalls edit %s --step-id %s", session.FlowID, session.StepID))
	default:
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 409 {
			// The edits were made against a stale revision, so they are never replayed automatically.
			rt.Out.Warn("Nothing was saved: the flow changed during the run (dashboard or another client).")
			rt.Out.Hint(fmt.Sprintf("Re-run against the current draft:  rc paywalls edit %s --step-id %s", session.FlowID, session.StepID))
		} else {
			rt.Out.Warn("Could not save the design to RevenueCat: " + err.Error())
			rt.Out.Hint("Nothing was saved. Re-run the command to try again.")
		}
	}
	if rt.Out.IsJSON() {
		return rt.Out.Render(map[string]any{
			"flow_id":          session.FlowID,
			"step_id":          session.StepID,
			"saved_steps":      saved,
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

// saveEditedSteps writes every step the run edited back in one draft update,
// guarded by the revision the run started from.
func saveEditedSteps(ctx context.Context, client *api.Client, projectID string, draft *api.FlowDraft, edited map[string]paywallai.PaywallData) ([]string, error) {
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
	if _, err := client.Flows.UpdateDraft(ctx, projectID, draft.ID, api.UpdateFlowDraft{
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
// editable paywall fields, which the server merges onto the stored step.
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
	return json.Marshal(map[string]any{"paywall": pw})
}

func requireScreenStep(draft *api.FlowDraft, stepID string) error {
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
