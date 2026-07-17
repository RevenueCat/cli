package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

// astraSession is the state file round-tripped between editor turns. Astra
// keeps no server-side paywall state between requests: the client must resend
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
	prompt      string
	offeringID  string
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
		Use:   "generate [offering-id]",
		Short: "Generate a paywall draft with the Paywall AI editor",
		Long: `Creates a draft paywall for an offering and designs it from a natural-language
prompt using Astra, the Paywall AI editor — the same engine behind the
dashboard's AI mode.

The editor state is saved to a session file so follow-up edits keep their
context: pass the same --session to rc paywalls edit. Astra may answer with a
clarifying question instead of a design — reply with another edit turn.

Limitation: the AI-designed components live in the session file only. The
public API cannot yet write paywall components, so rc paywalls publish would
publish the default template, not the design; finish in the dashboard editor.`,
		Example: `  rc paywalls generate
  rc paywalls generate ofrng_default --prompt "A calm annual-first paywall"
  rc paywalls generate ofrng_default --prompt "Match our brand" --image brand.png --json --no-input`,
		Args: cobra.MaximumNArgs(1),
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
			if argAt(args, 0) != "" {
				if opts.offeringID != "" && opts.offeringID != argAt(args, 0) {
					return fmt.Errorf("offering ID was provided both positionally and with --offering-id")
				}
				opts.offeringID = argAt(args, 0)
			}
			opts.offeringID, err = requireID(rt, opts.offeringID, "offering", func() ([]PickerItem, error) {
				return offeringPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			if err := requirePaywallAIPrompt(rt, &opts.prompt, "Describe the paywall you want"); err != nil {
				return err
			}

			paywall, err := client.Paywalls.Create(cmd.Context(), projectID, api.PaywallCreate{
				OfferingID:                 opts.offeringID,
				AutomaticallyScaleFontSize: true,
			})
			if err != nil {
				return fmt.Errorf("creating draft paywall: %w", err)
			}
			rt.Out.Info("Created draft paywall " + paywall.ID)

			offeringID := opts.offeringID
			session := &astraSession{
				Version:   1,
				ProjectID: projectID,
				PaywallID: paywall.ID,
				Paywall: astra.PaywallData{
					DefaultLocale:           "en_US",
					OfferingID:              &offeringID,
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
	return cmd
}

func newPaywallsEditCmd() *cobra.Command {
	opts := paywallAIOptions{
		prompt:  os.Getenv("RC_PAYWALL_PROMPT"),
		baseURL: envOrDefault("RC_ASTRA_BASE_URL", astra.DefaultBaseURL),
		timeout: 10 * time.Minute,
	}
	cmd := &cobra.Command{
		Use:   "edit --session <file>",
		Short: "Edit a paywall with the Paywall AI editor",
		Long: `Continues a Paywall AI editor session from its session file (written by
rc paywalls generate) and applies a natural-language edit.

Editing a paywall whose state only exists in the dashboard builder is not yet
supported from the CLI: the public API does not expose paywall component
state, so there is nothing to send the editor. Use the dashboard's AI mode
for those, or start fresh with rc paywalls generate.`,
		Example: `  rc paywalls edit --session pw_abc.astra.json --prompt "Make annual the visual default"
  rc paywalls edit --session pw_abc.astra.json --prompt "Add social proof" --json --no-input`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			if opts.sessionPath == "" {
				return fmt.Errorf("--session is required; rc paywalls generate writes it (see --help for why)")
			}
			session, err := loadAstraSession(opts.sessionPath)
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
	cmd.Flags().StringVar(&baseURL, "base-url", baseURL, "Astra endpoint (or RC_ASTRA_BASE_URL)")
	return cmd
}

func addPaywallAIFlags(cmd *cobra.Command, opts *paywallAIOptions) {
	cmd.Flags().StringVar(&opts.prompt, "prompt", opts.prompt, "natural-language direction (or RC_PAYWALL_PROMPT)")
	cmd.Flags().StringVar(&opts.sessionPath, "session", opts.sessionPath, "editor session file (default: <paywall-id>.astra.json)")
	cmd.Flags().StringArrayVar(&opts.images, "image", nil, "reference image to attach (png/jpeg/webp, max 3)")
	cmd.Flags().StringVar(&opts.baseURL, "base-url", opts.baseURL, "Astra endpoint (or RC_ASTRA_BASE_URL)")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", opts.timeout, "maximum time to wait")
}

// runPaywallAI streams one editor turn and persists the updated session file.
func runPaywallAI(ctx context.Context, rt *Runtime, opts paywallAIOptions, session *astraSession) error {
	client, err := astraClient(rt, opts.baseURL)
	if err != nil {
		return err
	}
	attachments, err := loadPaywallAIImages(opts.images)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, opts.timeout)
	defer cancel()

	rt.Out.Info("Starting Paywall AI editor run")
	stream, err := client.Stream(ctx, astra.EditorRequest{
		ProjectID:        session.ProjectID,
		PaywallID:        session.PaywallID,
		Revision:         session.Revision,
		SessionID:        session.SessionID,
		Paywall:          session.Paywall,
		UIConfig:         session.UIConfig,
		ProductVariables: session.ProductVariables,
		Message:          opts.prompt,
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
			return fmt.Errorf("Astra closed the stream without completing the run")
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
			return fmt.Errorf("Paywall AI editor run failed (%s): %s", event.Error.Code, event.Error.Message)
		case astra.EventRunCompleted:
			reportPaywallAIActivity(rt, event.Activity, reportedActivity)
			return finishPaywallAI(rt, opts, session, event)
		}
	}
}

func finishPaywallAI(rt *Runtime, opts paywallAIOptions, session *astraSession, event *astra.Event) error {
	session.SessionID = event.SessionID
	session.TraceID = event.TraceID
	if event.Paywall != nil {
		session.Paywall = *event.Paywall
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
	rt.Out.Success("Paywall design updated (draft " + session.PaywallID + ")")
	rt.Out.Info("Session saved to " + opts.sessionPath + " — continue with: rc paywalls edit --session " + opts.sessionPath)
	rt.Out.Warn("The AI design lives in the session file only: the public API cannot yet save paywall components, so publishing now would ship the default template. Finish the design in the dashboard paywall editor.")
	return rt.Out.Render(map[string]any{
		"paywall_id":   session.PaywallID,
		"session_id":   session.SessionID,
		"trace_id":     session.TraceID,
		"session_file": opts.sessionPath,
		"activity":     event.Activity,
	})
}

// reportPaywallAIActivity prints activity items not yet shown; snapshots carry
// the full list each time, so it resumes from the previous count.
func reportPaywallAIActivity(rt *Runtime, activity []astra.ToolActivity, alreadyReported int) int {
	for _, item := range activity[min(alreadyReported, len(activity)):] {
		switch item.Type {
		case "assistant_message":
			rt.Out.Info("Astra: " + item.Content)
		default:
			text := item.Display.Text
			if text == "" {
				text = item.ToolName
			}
			rt.Out.Info("⚙ " + text)
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
