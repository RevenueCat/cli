package cli

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/mcp"
	"github.com/revenuecat/cli/internal/tui"
)

const (
	createPaywallAITool = "create-paywall-ai"
	editPaywallAITool   = "edit-paywall-ai"
	getPaywallAITool    = "get-paywall-ai-task"
)

var pollSecondsPattern = regexp.MustCompile(`(?:in about|wait about) (\d+) seconds`)

type paywallAITask struct {
	TaskID          string `json:"task_id"`
	Kind            string `json:"kind,omitempty"`
	Status          string `json:"status"`
	StatusMessage   string `json:"status_message,omitempty"`
	ProjectID       string `json:"project_id,omitempty"`
	OfferingID      string `json:"offering_id,omitempty"`
	PaywallID       string `json:"paywall_id,omitempty"`
	EditorURL       string `json:"editor_url,omitempty"`
	Assistant       string `json:"assistant_message,omitempty"`
	ScreenshotCount int    `json:"screenshot_count,omitempty"`
	PollAfter       int    `json:"poll_after_seconds,omitempty"`
}

type paywallAIOptions struct {
	prompt     string
	context    string
	mcpURL     string
	async      bool
	timeout    time.Duration
	offeringID string
	paywallID  string
}

func newPaywallsGenerateCmd() *cobra.Command {
	opts := paywallAIOptions{
		prompt:     os.Getenv("RC_PAYWALL_PROMPT"),
		context:    os.Getenv("RC_PAYWALL_CONTEXT"),
		mcpURL:     envOrDefault("RC_MCP_URL", mcp.DefaultURL),
		offeringID: os.Getenv("RC_OFFERING_ID"),
		timeout:    12 * time.Minute,
	}
	cmd := &cobra.Command{
		Use:   "generate [offering-id]",
		Short: "Generate a paywall draft with Paywall AI Editor",
		Long: `Creates a RevenueCat paywall draft from a natural-language prompt using
Paywall AI Editor. Interactive terminals prompt for the direction and offering.
The command waits for the generated draft by default; pass --async to return the
persisted task ID immediately and inspect it later with rc paywalls task.`,
		Example: `  rc paywalls generate
  rc paywalls generate ofrng_default --prompt "A calm annual-first paywall" --json --no-input
  rc paywalls generate ofrng_default --prompt "Use our blue brand palette" --async --json --no-input`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if argAt(args, 0) != "" {
				if opts.offeringID != "" && opts.offeringID != argAt(args, 0) {
					return fmt.Errorf("offering ID was provided both positionally and with --offering-id")
				}
				opts.offeringID = argAt(args, 0)
			}
			if opts.offeringID == "" && tui.IsInteractive() && !rt.Globals.NoInput {
				opts.offeringID, err = chooseOptionalPaywallOffering(cmd.Context(), rt, projectID)
				if err != nil {
					return err
				}
			}
			if err := requirePaywallAIPrompt(rt, &opts.prompt, "Describe the paywall you want"); err != nil {
				return err
			}

			arguments := map[string]any{
				"project_id": projectID,
				"prompt":     opts.prompt,
			}
			if opts.offeringID != "" {
				arguments["offering_id"] = opts.offeringID
			}
			if opts.context != "" {
				arguments["codebase_context"] = opts.context
			}
			return runPaywallAITask(cmd.Context(), rt, opts, createPaywallAITool, arguments)
		},
	}
	addPaywallAIFlags(cmd, &opts)
	cmd.Flags().StringVar(&opts.offeringID, "offering-id", opts.offeringID, "offering to attach (or RC_OFFERING_ID)")
	return cmd
}

func newPaywallsEditCmd() *cobra.Command {
	opts := paywallAIOptions{
		prompt:    os.Getenv("RC_PAYWALL_PROMPT"),
		context:   os.Getenv("RC_PAYWALL_CONTEXT"),
		mcpURL:    envOrDefault("RC_MCP_URL", mcp.DefaultURL),
		paywallID: os.Getenv("RC_PAYWALL_ID"),
		timeout:   10 * time.Minute,
	}
	cmd := &cobra.Command{
		Use:   "edit [id]",
		Short: "Edit a paywall with Paywall AI Editor",
		Long: `Updates an existing paywall from a natural-language prompt using Paywall AI
Editor. A published paywall is copied into a new unpublished draft, so publishing
remains a separate explicit action. The command waits by default; pass --async
to return the persisted task ID immediately.`,
		Example: `  rc paywalls edit pw_abc
  rc paywalls edit pw_abc --prompt "Make annual the visual default" --json --no-input
  rc paywalls edit pw_abc --prompt "Replace the hero image" --async --json --no-input`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if argAt(args, 0) != "" {
				if opts.paywallID != "" && opts.paywallID != argAt(args, 0) {
					return fmt.Errorf("paywall ID was provided both positionally and with --paywall-id")
				}
				opts.paywallID = argAt(args, 0)
			}
			opts.paywallID, err = requireID(rt, opts.paywallID, "paywall", func() ([]PickerItem, error) {
				client, err := rt.API()
				if err != nil {
					return nil, err
				}
				return paywallPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			if err := requirePaywallAIPrompt(rt, &opts.prompt, "What should change?"); err != nil {
				return err
			}

			arguments := map[string]any{
				"project_id": projectID,
				"paywall_id": opts.paywallID,
				"prompt":     opts.prompt,
			}
			if opts.context != "" {
				arguments["codebase_context"] = opts.context
			}
			return runPaywallAITask(cmd.Context(), rt, opts, editPaywallAITool, arguments)
		},
	}
	addPaywallAIFlags(cmd, &opts)
	cmd.Flags().StringVar(&opts.paywallID, "paywall-id", opts.paywallID, "paywall to edit (or RC_PAYWALL_ID)")
	return cmd
}

func newPaywallsTaskCmd() *cobra.Command {
	var wait bool
	var timeout time.Duration
	var mcpURL = envOrDefault("RC_MCP_URL", mcp.DefaultURL)
	cmd := &cobra.Command{
		Use:   "task <task-id>",
		Short: "Inspect a Paywall AI Editor task",
		Example: `  rc paywalls task task_123 --json --no-input
  rc paywalls task task_123 --wait --timeout 10m --json --no-input`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			client, err := paywallAIMCPClient(rt, mcpURL)
			if err != nil {
				return err
			}
			if wait {
				task := &paywallAITask{TaskID: args[0], Status: "queued"}
				finished, err := waitForPaywallAI(cmd.Context(), rt, client, task, timeout)
				if err != nil {
					return err
				}
				return renderPaywallAITask(rt, finished)
			}
			task, err := getPaywallAITask(cmd.Context(), client, args[0])
			if err != nil {
				return err
			}
			return renderPaywallAITask(rt, task)
		},
	}
	cmd.Flags().BoolVar(&wait, "wait", false, "wait until the task succeeds or fails")
	cmd.Flags().DurationVar(&timeout, "timeout", 12*time.Minute, "maximum time to wait")
	cmd.Flags().StringVar(&mcpURL, "mcp-url", mcpURL, "RevenueCat MCP endpoint (or RC_MCP_URL)")
	return cmd
}

func addPaywallAIFlags(cmd *cobra.Command, opts *paywallAIOptions) {
	cmd.Flags().StringVar(&opts.prompt, "prompt", opts.prompt, "natural-language direction (or RC_PAYWALL_PROMPT)")
	cmd.Flags().StringVar(&opts.context, "context", opts.context, "app or codebase context for the editor (or RC_PAYWALL_CONTEXT)")
	cmd.Flags().StringVar(&opts.mcpURL, "mcp-url", opts.mcpURL, "RevenueCat MCP endpoint (or RC_MCP_URL)")
	cmd.Flags().BoolVar(&opts.async, "async", false, "return the persisted task ID without waiting")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", opts.timeout, "maximum time to wait")
}

func runPaywallAITask(ctx context.Context, rt *Runtime, opts paywallAIOptions, tool string, arguments map[string]any) error {
	client, err := paywallAIMCPClient(rt, opts.mcpURL)
	if err != nil {
		return err
	}
	action := "generation"
	if tool == editPaywallAITool {
		action = "edit"
	}
	rt.Out.Info("Starting Paywall AI Editor " + action)
	result, err := client.CallTool(ctx, tool, arguments)
	if err != nil {
		return err
	}
	task, err := parsePaywallAITask("", result)
	if err != nil {
		return err
	}
	if opts.async {
		rt.Out.Success(fmt.Sprintf("Started Paywall AI Editor task %s", task.TaskID))
		return renderPaywallAITask(rt, task)
	}
	finished, err := waitForPaywallAI(ctx, rt, client, task, opts.timeout)
	if err != nil {
		return err
	}
	return renderPaywallAITask(rt, finished)
}

type paywallAIToolCaller interface {
	CallTool(context.Context, string, map[string]any) (*mcp.ToolResult, error)
}

func waitForPaywallAI(ctx context.Context, rt *Runtime, client paywallAIToolCaller, task *paywallAITask, timeout time.Duration) (*paywallAITask, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	lastStatus := task.Status
	lastStatusMessage := task.StatusMessage
	for {
		current, err := getPaywallAITask(ctx, client, task.TaskID)
		if err != nil {
			return nil, err
		}
		if current.Status != lastStatus || current.StatusMessage != lastStatusMessage {
			rt.Out.Info(fmt.Sprintf("Paywall AI Editor: %s — %s", current.Status, current.StatusMessage))
			lastStatus = current.Status
			lastStatusMessage = current.StatusMessage
		}
		if current.Status == "succeeded" {
			rt.Out.Success(fmt.Sprintf("Paywall draft %s is ready", current.PaywallID))
			return current, nil
		}
		if current.Status == "failed" {
			return nil, fmt.Errorf("Paywall AI Editor task %s failed: %s", current.TaskID, current.StatusMessage)
		}
		delay := time.Duration(current.PollAfter) * time.Second
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out waiting for Paywall AI Editor task %s: %w", task.TaskID, ctx.Err())
		case <-time.After(delay):
		}
	}
}

func getPaywallAITask(ctx context.Context, client paywallAIToolCaller, taskID string) (*paywallAITask, error) {
	result, err := client.CallTool(ctx, getPaywallAITool, map[string]any{"task_id": taskID})
	if err != nil {
		return nil, err
	}
	return parsePaywallAITask(taskID, result)
}

func parsePaywallAITask(taskID string, result *mcp.ToolResult) (*paywallAITask, error) {
	text := result.Text()
	task := &paywallAITask{
		TaskID:          firstNonEmpty(taskID, textField(text, "Task")),
		Status:          textField(text, "Status"),
		StatusMessage:   textField(text, "Message"),
		ProjectID:       textField(text, "Project"),
		OfferingID:      textField(text, "Offering"),
		PaywallID:       textField(text, "Paywall"),
		EditorURL:       textField(text, "Editor"),
		Assistant:       textField(text, "Assistant"),
		ScreenshotCount: result.ImageCount(),
		PollAfter:       parsePollSeconds(text),
	}
	if task.OfferingID == "none" {
		task.OfferingID = ""
	}
	if strings.Contains(text, "creation started") {
		task.Kind = "create"
	}
	if strings.Contains(text, "edit started") {
		task.Kind = "edit"
	}
	if strings.HasPrefix(text, "Paywall created") {
		task.Kind = "create"
		task.Status = "succeeded"
		task.PollAfter = 0
	}
	if strings.HasPrefix(text, "Paywall updated") {
		task.Kind = "edit"
		task.Status = "succeeded"
		task.PollAfter = 0
	}
	if task.TaskID == "" {
		return nil, fmt.Errorf("Paywall AI Editor response did not include a task ID")
	}
	if task.Status == "" {
		return nil, fmt.Errorf("Paywall AI Editor response did not include task status")
	}
	return task, nil
}

func textField(text, name string) string {
	prefix := name + ":"
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func parsePollSeconds(text string) int {
	match := pollSecondsPattern.FindStringSubmatch(text)
	if len(match) != 2 {
		return 1
	}
	seconds, err := strconv.Atoi(match[1])
	if err != nil {
		return 1
	}
	return seconds
}

func renderPaywallAITask(rt *Runtime, task *paywallAITask) error {
	if task.EditorURL != "" {
		rt.Out.Info("Review the draft: " + task.EditorURL)
	}
	if task.Status == "succeeded" {
		rt.Out.Info(fmt.Sprintf("Publish after review with: rc paywalls publish %s", task.PaywallID))
	}
	return rt.Out.Render(task)
}

func paywallAIMCPClient(rt *Runtime, url string) (*mcp.Client, error) {
	if _, err := rt.API(); err != nil {
		return nil, err
	}
	return mcp.NewClient(mcp.Options{
		URL:       url,
		Token:     rt.Config.BearerToken(),
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

func chooseOptionalPaywallOffering(ctx context.Context, rt *Runtime, projectID string) (string, error) {
	client, err := rt.API()
	if err != nil {
		return "", err
	}
	page, err := client.Offerings.List(ctx, projectID)
	if err != nil {
		return "", err
	}
	options := []huh.Option[string]{huh.NewOption("No offering (unattached draft)", "")}
	for _, offering := range page.Items {
		label := offering.LookupKey
		if offering.DisplayName != "" {
			label = offering.DisplayName + " (" + offering.LookupKey + ")"
		}
		options = append(options, huh.NewOption(label, offering.ID))
	}
	var offeringID string
	selectField := huh.NewSelect[string]().
		Title("Attach to an offering").
		Options(options...).
		Filtering(true).
		Value(&offeringID)
	if err := tui.Form(false).Field(selectField).Run(); err != nil {
		return "", err
	}
	return offeringID, nil
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
