package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/astra"
	"github.com/revenuecat/cli/internal/tui"
)

// astraSession is the state file round-tripped between editor turns. The
// Paywall AI editor keeps no server-side paywall state between requests: the client must resend
// the full paywall plus the opaque session blobs every turn, so the CLI
// persists them here (the dashboard holds the same data in builder state).
type astraSession struct {
	Version          int               `json:"version"`
	ProjectID        string            `json:"project_id"`
	PaywallID        string            `json:"paywall_id"`
	SessionID        string            `json:"session_id,omitempty"`
	TraceID          string            `json:"trace_id,omitempty"`
	Revision         *int              `json:"revision"`
	Paywall          astra.PaywallData `json:"paywall"`
	UIConfig         json.RawMessage   `json:"ui_config"`
	ProductVariables map[string]string `json:"product_variables"`
	SessionItems     json.RawMessage   `json:"__unstable_session_items"`
	AppContext       json.RawMessage   `json:"app_context,omitempty"`
}

// Minimal valid editor state for a brand-new paywall, mirroring the
// dashboard's buildMinimalPaywall test helper: an empty root stack on a
// white background, no fonts or presets yet.
const minimalComponentsConfig = `{
  "base": {
    "stack": {
      "id": "root",
      "type": "stack",
      "dimension": {"type": "vertical", "alignment": "center", "distribution": "start"},
      "size": {"width": {"type": "fill"}, "height": {"type": "fill"}},
      "margin": {"top": 0, "bottom": 0, "leading": 0, "trailing": 0},
      "padding": {"top": 0, "bottom": 0, "leading": 0, "trailing": 0},
      "components": []
    },
    "sticky_footer": null,
    "background": {"type": "color", "value": {"light": {"type": "hex", "value": "#FFFFFF"}}},
    "header": null
  }
}`

const minimalUIConfig = `{"fonts": {}, "presets": {"saved_colors": []}}`

type paywallAIOptions struct {
	context     string
	attachments []string
	prompt      string
	offeringID  string
	name        string
	sessionPath string
	images      []string
	baseURL     string
	timeout     time.Duration
}

func newPaywallsGenerateCmd() *cobra.Command {
	opts := paywallAIOptions{
		prompt:     os.Getenv("RC_PAYWALL_PROMPT"),
		offeringID: os.Getenv("RC_OFFERING_ID"),
		baseURL:    envOrDefault("RC_ASTRA_BASE_URL", astra.DefaultBaseURL),
		timeout:    12 * time.Minute,
	}
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a paywall draft with the Paywall AI editor",
		Long: `Creates a draft paywall and designs it from a natural-language prompt using
the Paywall AI editor — the same engine behind the dashboard's AI mode.

The draft is standalone unless --offering-id attaches it to an offering; an
offering can only have one paywall, so attaching fails if it already has one.

Each completed turn saves the design onto the RevenueCat paywall draft, and
the editor state is also kept in a session file so follow-up edits retain the
conversation: pass the same --session to rc paywalls edit. The editor may
answer with a clarifying question instead of a design — reply with another
edit turn. The draft stays unpublished; review it and run rc paywalls publish.`,
		Example: `  rc paywalls generate
  rc paywalls generate --name "Summer sale" --prompt "A calm annual-first paywall"
  rc paywalls generate --offering-id ofrng_default --prompt "Match our brand" --image brand.png --json --no-input`,
		Args: cobra.NoArgs,
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
			if err := requirePaywallAIPrompt(rt, &opts.prompt, "Describe the paywall you want"); err != nil {
				return err
			}

			paywall, err := client.Paywalls.CreateFromComponents(cmd.Context(), projectID, api.PaywallComponentsCreate{
				OfferingID:              opts.offeringID,
				Name:                    opts.name,
				ComponentsConfig:        json.RawMessage(minimalComponentsConfig),
				ComponentsLocalizations: json.RawMessage(`{"en_US": {}}`),
				DefaultLocale:           "en_US",
			})
			if err != nil {
				// An offering can only have one paywall; the server reports
				// that as a bare 409.
				var apiErr *api.APIError
				if opts.offeringID != "" && errors.As(err, &apiErr) && apiErr.Status == 409 {
					rt.Out.Hint("Generate against another offering, or omit --offering-id to create a standalone draft and attach it in the dashboard later")
					rt.Out.Hint("Or delete the existing paywall:  rc paywalls list, then rc paywalls delete <id>")
					return fmt.Errorf("creating draft paywall: offering %s already has a paywall (an offering can only have one): %w", opts.offeringID, err)
				}
				return fmt.Errorf("creating draft paywall: %w", err)
			}
			rt.Out.Info("Created draft paywall " + paywall.ID)

			var offeringID *string
			if opts.offeringID != "" {
				offeringID = &opts.offeringID
			}
			session := &astraSession{
				Version:   1,
				ProjectID: projectID,
				PaywallID: paywall.ID,
				Paywall: astra.PaywallData{
					DefaultLocale:           "en_US",
					OfferingID:              offeringID,
					ComponentsConfig:        json.RawMessage(minimalComponentsConfig),
					ComponentsLocalizations: json.RawMessage(`{"en_US": {}}`),
				},
				UIConfig:         json.RawMessage(minimalUIConfig),
				ProductVariables: map[string]string{},
				SessionItems:     json.RawMessage(`{}`),
			}
			if opts.sessionPath == "" {
				opts.sessionPath = paywall.ID + ".astra.json"
			}
			return runPaywallAI(cmd.Context(), rt, opts, session)
		},
	}
	addPaywallAIFlags(cmd, &opts)
	cmd.Flags().StringVar(&opts.offeringID, "offering-id", opts.offeringID, "offering to attach (or RC_OFFERING_ID)")
	cmd.Flags().StringVar(&opts.name, "name", "", "paywall name")
	return cmd
}

func newPaywallsEditCmd() *cobra.Command {
	opts := paywallAIOptions{
		prompt:  os.Getenv("RC_PAYWALL_PROMPT"),
		baseURL: envOrDefault("RC_ASTRA_BASE_URL", astra.DefaultBaseURL),
		timeout: 10 * time.Minute,
	}
	cmd := &cobra.Command{
		Use:   "edit [paywall-id]",
		Short: "Edit a paywall with the Paywall AI editor",
		Long: `Applies a natural-language edit to a paywall.

Given a paywall ID, the current draft (or published) components are fetched
from RevenueCat as the starting state — any paywall is editable, including
dashboard-authored ones. Given --session (written by a previous generate or
edit turn), the design conversation continues with its full context. Each
completed turn saves the design back onto the RevenueCat draft
(revision-guarded) and prints the dashboard builder URL to view it.

Using it well:

  - Write a SPECIFIC prompt: exact hex colors, font vibe, tone words, real
    feature names, and what to avoid. Read the app's theme files and the
    session JSON for grounding first — but only ever CHANGE the design
    through this command; hand-editing the session file gets clobbered by
    the next turn or fails the revision guard.
  - Pass design references: --attachment DESIGN.md folds text style guides
    into the direction; --attachment screenshot.png (or --image) attaches
    visually, up to 3 images total.
  - Keep the SAME --session file across turns — it is the conversation
    memory. A lost session file starts the design conversation over.
  - The Paywall AI editor may reply with a clarifying question instead of a design (it
    appears in the streamed activity / the --json activity array). Answer it
    with another edit turn on the same session.
  - Turns take one to several minutes and stream progress; run with an
    extended timeout (--timeout) or in the background rather than polling.
  - Undo the last turn:  rc paywalls rewind --session <file>`,
		Example: `  rc paywalls edit                       # picker, then it asks what to change
  rc paywalls edit pw_abc --prompt "Background #0E1B2A, bolder CTA, keep the layout"
  rc paywalls edit pw_abc --prompt "match this" --attachment DESIGN.md --image home.png
  rc paywalls edit --session pw_abc.astra.json --prompt "Push the gradient harder" --json --no-input`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			var session *astraSession
			var err error
			switch {
			case opts.sessionPath != "":
				session, err = loadAstraSession(opts.sessionPath)
				if err == nil {
					session, err = preflightSessionRevision(cmd.Context(), rt, session)
				}
			case argAt(args, 0) != "" || (!rt.Globals.NoInput && tui.IsInteractive()):
				client, cerr := rt.API()
				if cerr != nil {
					return cerr
				}
				projectID, perr := requireProject(rt)
				if perr != nil {
					return perr
				}
				paywallID, perr := requireID(rt, argAt(args, 0), "paywall", func() ([]PickerItem, error) {
					return paywallPickerItems(cmd.Context(), client, projectID)
				})
				if perr != nil {
					return perr
				}
				session, err = seedSessionFromServer(cmd.Context(), rt, paywallID)
				opts.sessionPath = paywallID + ".astra.json"
			default:
				return fmt.Errorf("pass a paywall ID or --session <file>")
			}
			if err != nil {
				return err
			}
			if err := requirePaywallAIPrompt(rt, &opts.prompt, "What should change?"); err != nil {
				return err
			}
			return runPaywallAI(cmd.Context(), rt, opts, session)
		},
	}
	addPaywallAIFlags(cmd, &opts)
	return cmd
}

// preflightSessionRevision guards a file-loaded session against a draft that
// changed elsewhere (dashboard, its AI editor, API) since the file was
// written. Turns take minutes, so the revisions are compared before anything
// is streamed. A diverged session can't be continued — the prompt was written
// against state that no longer exists — so the only way forward is consent to
// start fresh from the server's current draft, losing the conversation.
func preflightSessionRevision(ctx context.Context, rt *Runtime, session *astraSession) (*astraSession, error) {
	if session.Revision == nil {
		return session, nil
	}
	client, err := rt.API()
	if err != nil {
		return nil, err
	}
	revision, err := currentDraftRevision(ctx, client, session.ProjectID, session.PaywallID)
	if err != nil {
		return nil, err
	}
	if revision == *session.Revision {
		return session, nil
	}
	rt.Out.Warn(fmt.Sprintf("The draft for %s changed outside this session — the dashboard, its AI editor, or the API wrote revision %d, the session has %d.", session.PaywallID, revision, *session.Revision))
	rt.Out.Info("A session can't continue against diverged state. Continuing starts fresh from the server's current draft; the conversation context in this session file is lost.")
	if err := confirmOrAbort(rt, "Start fresh from the server's current draft?",
		"run rc paywalls edit "+session.PaywallID+" to start fresh deliberately"); err != nil {
		return nil, err
	}
	return seedSessionFromServer(ctx, rt, session.PaywallID)
}

// seedSessionFromServer starts an editor session from the paywall's current
// RevenueCat state (draft components, falling back to published).
func seedSessionFromServer(ctx context.Context, rt *Runtime, paywallID string) (*astraSession, error) {
	projectID, err := requireProject(rt)
	if err != nil {
		return nil, err
	}
	client, err := rt.API()
	if err != nil {
		return nil, err
	}
	paywall, err := client.Paywalls.GetWithComponents(ctx, projectID, paywallID)
	if err != nil {
		return nil, err
	}
	version := (*api.PaywallComponentsVersion)(nil)
	if paywall.Components != nil {
		version = paywall.Components.Draft
		if version == nil {
			version = paywall.Components.Published
		}
	}
	if version == nil || len(version.ComponentsConfig) == 0 {
		return nil, fmt.Errorf("paywall %s has no component state to edit; design one with rc paywalls generate", paywallID)
	}
	locale := version.DefaultLocale
	if locale == "" {
		locale = "en_US"
	}
	localizations := version.ComponentsLocalizations
	if len(localizations) == 0 {
		localizations = json.RawMessage(`{"` + locale + `": {}}`)
	}
	var offeringID *string
	if paywall.OfferingID != "" {
		offeringID = &paywall.OfferingID
	}
	return &astraSession{
		Version:   1,
		ProjectID: projectID,
		PaywallID: paywall.ID,
		Revision:  version.Revision,
		Paywall: astra.PaywallData{
			DefaultLocale:           locale,
			OfferingID:              offeringID,
			ComponentsConfig:        version.ComponentsConfig,
			ComponentsLocalizations: localizations,
		},
		UIConfig:         json.RawMessage(minimalUIConfig),
		ProductVariables: map[string]string{},
		SessionItems:     json.RawMessage(`{}`),
	}, nil
}

func newPaywallsRewindCmd() *cobra.Command {
	var sessionPath string
	baseURL := envOrDefault("RC_ASTRA_BASE_URL", astra.DefaultBaseURL)
	cmd := &cobra.Command{
		Use:   "rewind --session <file>",
		Short: "Undo the last Paywall AI editor action",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			if sessionPath == "" {
				return fmt.Errorf("--session is required")
			}
			session, err := loadAstraSession(sessionPath)
			if err != nil {
				return err
			}
			if session.SessionID == "" || session.TraceID == "" {
				return fmt.Errorf("session file has no completed run to rewind")
			}
			client, err := astraClient(rt, baseURL)
			if err != nil {
				return err
			}
			if err := client.Rewind(cmd.Context(), session.SessionID, session.TraceID, true); err != nil {
				return err
			}
			rt.Out.Success("Rewound last editor action")
			return rt.Out.Render(map[string]any{"ok": true, "session_id": session.SessionID})
		},
	}
	cmd.Flags().StringVar(&sessionPath, "session", "", "editor session file")
	cmd.Flags().StringVar(&baseURL, "base-url", baseURL, "Paywall AI editor endpoint (or RC_ASTRA_BASE_URL)")
	return cmd
}

func addPaywallAIFlags(cmd *cobra.Command, opts *paywallAIOptions) {
	cmd.Flags().StringVar(&opts.prompt, "prompt", opts.prompt, "natural-language direction (or RC_PAYWALL_PROMPT)")
	cmd.Flags().StringVar(&opts.context, "context", "", "product/audience/brand context sent alongside the direction")
	cmd.Flags().StringArrayVar(&opts.attachments, "attachment", nil, "design reference file: images (png/jpeg/webp) attach visually, text files (DESIGN.md, style guides) travel with the direction")
	cmd.Flags().StringVar(&opts.sessionPath, "session", opts.sessionPath, "editor session file (default: <paywall-id>.astra.json)")
	cmd.Flags().StringArrayVar(&opts.images, "image", nil, "reference image to attach (png/jpeg/webp, max 3)")
	cmd.Flags().StringVar(&opts.baseURL, "base-url", opts.baseURL, "Paywall AI editor endpoint (or RC_ASTRA_BASE_URL)")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", opts.timeout, "maximum time to wait")
}

// runPaywallAI streams one editor turn and persists the updated session file.
func runPaywallAI(ctx context.Context, rt *Runtime, opts paywallAIOptions, session *astraSession) error {
	client, err := astraClient(rt, opts.baseURL)
	if err != nil {
		return err
	}
	attachments, textRefs, err := loadPaywallAIAttachments(opts.images, opts.attachments)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, opts.timeout)
	defer cancel()

	rt.Out.Info("Designing with the Paywall AI editor — this can take a few minutes…")
	stream, err := client.Stream(ctx, astra.EditorRequest{
		ProjectID:        session.ProjectID,
		PaywallID:        session.PaywallID,
		Revision:         session.Revision,
		SessionID:        session.SessionID,
		Paywall:          session.Paywall,
		UIConfig:         session.UIConfig,
		ProductVariables: session.ProductVariables,
		Message:          withPaywallContext(opts.prompt, opts.context) + textRefs,
		InputAttachments: attachments,
		SessionItems:     session.SessionItems,
		AppContext:       session.AppContext,
	})
	if err != nil {
		return err
	}
	defer stream.Close()

	reportedActivity := 0
	for {
		event, err := stream.Next()
		if err == io.EOF {
			return fmt.Errorf("astra closed the stream without completing the run")
		}
		if err != nil {
			return err
		}
		switch event.Type {
		case astra.EventRunStarted:
			session.SessionID = event.SessionID
		case astra.EventTurnSnapshot:
			reportedActivity = reportPaywallAIActivity(rt, event.Activity, reportedActivity)
		case astra.EventRunFailed:
			return fmt.Errorf("paywall AI editor run failed (%s): %s", event.Error.Code, event.Error.Message)
		case astra.EventRunCompleted:
			reportPaywallAIActivity(rt, event.Activity, reportedActivity)
			return finishPaywallAI(ctx, rt, opts, session, event)
		}
	}
}

func finishPaywallAI(ctx context.Context, rt *Runtime, opts paywallAIOptions, session *astraSession, event *astra.Event) error {
	session.SessionID = event.SessionID
	session.TraceID = event.TraceID
	if event.Paywall != nil {
		// The AI editor doesn't manage offering attachment and echoes
		// offering_id as null — keep what the CLI established.
		offeringID := session.Paywall.OfferingID
		session.Paywall = *event.Paywall
		if session.Paywall.OfferingID == nil {
			session.Paywall.OfferingID = offeringID
		}
	}
	if len(event.SessionItems) > 0 {
		session.SessionItems = event.SessionItems
	}
	if len(event.AppContext) > 0 {
		session.AppContext = event.AppContext
	}
	if err := saveAstraSession(opts.sessionPath, session); err != nil {
		return err
	}
	saved := false
	if err := persistPaywallDesign(ctx, rt, session); err != nil {
		var apiErr *api.APIError
		if errors.As(err, &apiErr) && apiErr.Status == 409 {
			rt.Out.Warn("Could not save the design: the draft changed during the run (dashboard, its AI editor, or API), and this session can't be saved over it.")
			rt.Out.Hint("Start fresh from the current draft:  rc paywalls edit " + session.PaywallID)
		} else {
			rt.Out.Warn("Could not save the design to RevenueCat: " + err.Error())
			rt.Out.Hint("The design is safe in " + opts.sessionPath + " — re-run rc paywalls edit to retry saving.")
		}
	} else {
		saved = true
		// Persisting refreshed the revision and offering from the server;
		// re-save so the next turn starts from them.
		if err := saveAstraSession(opts.sessionPath, session); err != nil {
			return err
		}
		rt.Out.Success("Design saved to paywall draft " + session.PaywallID)
		if errored := countErroredActivity(event.Activity); errored > 0 {
			rt.Out.Info(fmt.Sprintf("%d editor step(s) errored during the run and were retried by the Paywall AI editor — the saved draft is the complete final state (nothing partial is ever saved).", errored))
		}
		rt.Out.Blank()
		rt.Out.Field("View it", paywallBuilderURL(session.ProjectID, session.PaywallID))
		rt.Out.Field("Keep designing", "rc paywalls edit --session "+opts.sessionPath)
		if session.Paywall.OfferingID == nil {
			rt.Out.Field("Attach it", "dashboard → Paywalls → "+session.PaywallID+" → attach to an offering")
		} else {
			rt.Out.Field("Publish when ready", "rc paywalls publish "+session.PaywallID)
		}
	}
	if rt.Out.IsJSON() {
		return rt.Out.Render(map[string]any{
			"paywall_id":     session.PaywallID,
			"dashboard_url":  paywallBuilderURL(session.ProjectID, session.PaywallID),
			"session_id":     session.SessionID,
			"trace_id":       session.TraceID,
			"session_file":   opts.sessionPath,
			"saved_to_draft": saved,
			"activity":       event.Activity,
		})
	}
	return nil
}

// paywallBuilderURL is the dashboard's visual editor for a components
// paywall — the thing to look at after a design turn.
func paywallBuilderURL(projectID, paywallID string) string {
	return fmt.Sprintf("https://app.revenuecat.com/projects/%s/paywalls/%s/builder", dashboardProjectID(projectID), paywallID)
}

// persistPaywallDesign PATCHes the designed components onto the RevenueCat
// paywall draft, guarded by the session's own revision: a draft that changed
// outside the session comes back as a 409 instead of being overwritten.
func persistPaywallDesign(ctx context.Context, rt *Runtime, session *astraSession) error {
	client, err := rt.API()
	if err != nil {
		return err
	}
	update := api.PaywallDraftUpdate{
		ComponentsConfig:        session.Paywall.ComponentsConfig,
		ComponentsLocalizations: session.Paywall.ComponentsLocalizations,
		DefaultLocale:           session.Paywall.DefaultLocale,
	}
	if session.Revision != nil {
		update.Revision = *session.Revision
	} else {
		// First turn from rc paywalls generate: the session exists before any
		// PATCH, and the paywall was created moments ago.
		revision, err := currentDraftRevision(ctx, client, session.ProjectID, session.PaywallID)
		if err != nil {
			return err
		}
		update.Revision = revision
	}
	updated, err := client.Paywalls.UpdateDraft(ctx, session.ProjectID, session.PaywallID, update)
	if err != nil {
		return err
	}
	if updated.Components != nil && updated.Components.Draft != nil && updated.Components.Draft.Revision != nil {
		session.Revision = updated.Components.Draft.Revision
	}
	// Offering attachment can change out-of-band (dashboard); the PATCH
	// response carries current server truth, so refresh it — it drives
	// the attach/publish hint and the editor's product context next turn.
	if updated.OfferingID != "" {
		session.Paywall.OfferingID = &updated.OfferingID
	} else {
		session.Paywall.OfferingID = nil
	}
	return nil
}

func currentDraftRevision(ctx context.Context, client *api.Client, projectID, paywallID string) (int, error) {
	paywall, err := client.Paywalls.GetWithComponents(ctx, projectID, paywallID)
	if err != nil {
		return 0, err
	}
	if paywall.Components != nil {
		if d := paywall.Components.Draft; d != nil && d.Revision != nil {
			return *d.Revision, nil
		}
		if p := paywall.Components.Published; p != nil && p.Revision != nil {
			return *p.Revision, nil
		}
	}
	return 0, nil
}

// reportPaywallAIActivity prints activity items not yet shown; snapshots carry
// the full list each time, so it resumes from the previous count.
func reportPaywallAIActivity(rt *Runtime, activity []astra.ToolActivity, alreadyReported int) int {
	for _, item := range activity[min(alreadyReported, len(activity)):] {
		switch item.Type {
		case "assistant_message":
			rt.Out.Info("Paywall AI: " + item.Content)
		default:
			text := item.Display.Text
			if text == "" {
				text = item.ToolName
			}
			if item.Status == "error" {
				rt.Out.Warn("⚙ " + text + "  (errored — the Paywall AI editor retries these itself)")
			} else {
				rt.Out.Info("⚙ " + text)
			}
		}
	}
	if len(activity) > alreadyReported {
		return len(activity)
	}
	return alreadyReported
}

func loadAstraSession(path string) (*astraSession, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading session file: %w", err)
	}
	var session astraSession
	if err := json.Unmarshal(payload, &session); err != nil {
		return nil, fmt.Errorf("parsing session file %s: %w", path, err)
	}
	if session.ProjectID == "" || session.PaywallID == "" {
		return nil, fmt.Errorf("session file %s is missing project_id or paywall_id", path)
	}
	return &session, nil
}

func saveAstraSession(path string, session *astraSession) error {
	payload, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(payload, '\n'), 0o600)
}

var paywallAIImageTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
}

func loadPaywallAIImages(paths []string) ([]astra.InputAttachment, error) {
	if len(paths) > 3 {
		return nil, fmt.Errorf("at most 3 images can be attached, got %d", len(paths))
	}
	var attachments []astra.InputAttachment
	for _, path := range paths {
		mimeType, ok := paywallAIImageTypes[strings.ToLower(filepath.Ext(path))]
		if !ok {
			return nil, fmt.Errorf("unsupported image type %q (png, jpeg, webp)", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if len(data) > 10<<20 {
			return nil, fmt.Errorf("image %s exceeds the 10MB limit", path)
		}
		attachments = append(attachments, astra.InputAttachment{
			Type:       "image",
			Filename:   filepath.Base(path),
			MimeType:   mimeType,
			DataBase64: base64.StdEncoding.EncodeToString(data),
		})
	}
	return attachments, nil
}

func astraClient(rt *Runtime, baseURL string) (*astra.Client, error) {
	// rt.API() refreshes the OAuth token if needed and enforces login.
	if _, err := rt.API(); err != nil {
		return nil, err
	}
	return astra.NewClient(astra.Options{
		BaseURL:   baseURL,
		Token:     agentAuthToken(rt),
		UserAgent: userAgent(rt.Globals.Version),
	}), nil
}

func requirePaywallAIPrompt(rt *Runtime, prompt *string, title string) error {
	if *prompt != "" {
		return nil
	}
	if rt.Globals.NoInput || !tui.IsInteractive() {
		return fmt.Errorf("prompt is required; pass --prompt or set RC_PAYWALL_PROMPT")
	}
	return tui.Form(rt.Globals.NoInput).
		Field(huh.NewInput().Title(title).Value(prompt).Validate(tui.Required("prompt"))).
		Run()
}

// withPaywallContext folds --context into the direction sent to the Paywall AI editor: the
// skills document the flag (product/audience/brand context) and agents were
// burning generation attempts on unknown-flag errors before hand-merging it.
func withPaywallContext(prompt, context string) string {
	if context == "" {
		return prompt
	}
	return "Context: " + context + "\n\nDirection: " + prompt
}

func countErroredActivity(activity []astra.ToolActivity) int {
	n := 0
	for _, item := range activity {
		if item.Status == "error" {
			n++
		}
	}
	return n
}

// loadPaywallAIAttachments routes --image and --attachment by type: image
// files become real Paywall AI editor attachments (the server accepts only
// png/jpeg/webp, max 3, 10MB); text design references (DESIGN.md, style
// guides) are folded into the message, which is the only channel the editor
// API has for them today. Anything else (fonts, binaries) errors clearly.
func loadPaywallAIAttachments(images, attachments []string) ([]astra.InputAttachment, string, error) {
	imagePaths := append([]string(nil), images...)
	var textBlocks []string
	for _, path := range attachments {
		if _, ok := paywallAIImageTypes[strings.ToLower(filepath.Ext(path))]; ok {
			imagePaths = append(imagePaths, path)
			continue
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".md", ".txt", ".json", ".yaml", ".yml", ".css":
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, "", fmt.Errorf("read attachment: %w", err)
			}
			if len(data) > 64*1024 {
				return nil, "", fmt.Errorf("text attachment %s is %dKB; keep design references under 64KB", path, len(data)/1024)
			}
			textBlocks = append(textBlocks, "Design reference ("+filepath.Base(path)+"):\n```\n"+string(data)+"\n```")
		default:
			return nil, "", fmt.Errorf("unsupported attachment %q: the Paywall AI editor accepts images (png/jpeg/webp) and text design references (md/txt/json/yaml/css) — fonts and other binaries are not supported by the editor API yet", path)
		}
	}
	loaded, err := loadPaywallAIImages(imagePaths)
	if err != nil {
		return nil, "", err
	}
	textRefs := ""
	if len(textBlocks) > 0 {
		textRefs = "\n\n" + strings.Join(textBlocks, "\n\n")
	}
	return loaded, textRefs, nil
}
