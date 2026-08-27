package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/rico"
	"github.com/revenuecat/cli/internal/tui"
)

// ricoResumeRounds caps interrupt-approval loops within one chat turn so a
// misbehaving run can't hold the CLI forever.
const ricoResumeRounds = 10

func newRicoCmd() *cobra.Command {
	opts := ricoChatOptions{
		prompt:         os.Getenv("RC_RICO_PROMPT"),
		conversationID: os.Getenv("RC_RICO_CONVERSATION_ID"),
		baseURL:        devEnvOrDefault("RC_RICO_BASE_URL", rico.DefaultBaseURL),
		timeout:        10 * time.Minute,
	}
	cmd := &cobra.Command{
		Use:   "rico [message]",
		Short: "Chat with Rico, RevenueCat's AI assistant",
		Long: `Rico answers questions about your projects, metrics, and configuration, and
can run RevenueCat tools on your behalf. Tool calls that change data pause for
approval before executing.

With no message in a terminal, opens a full-screen chat window; with a message
(or --prompt) it answers once and exits. Pass --print to force a single answer
for scripts and agents, or --plain for a line-based loop.

Conversations are stored server-side: --continue resumes the most recent one,
--resume picks from a list, and --conversation <id> continues a specific one.
Tool calls that modify data pause for approval; non-interactive runs reject
them unless --approve-tools is passed (destructive tools also require --yes).`,
		Example: `  rc rico
  rc rico "why did trial conversions drop this week?"
  rc rico --continue
  rc rico --resume
  rc rico --print "how many active subscriptions do we have?" --json
  rc rico "delete the test offering" --approve-tools --yes --no-input`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			if argAt(args, 0) != "" {
				if opts.prompt != "" && opts.prompt != argAt(args, 0) {
					return fmt.Errorf("message was provided both positionally and with --prompt")
				}
				opts.prompt = argAt(args, 0)
			}
			client, err := ricoClient(rt, opts.baseURL)
			if err != nil {
				return err
			}
			if opts.continueLast && opts.conversationID == "" {
				var state ricoState
				_ = config.LoadState(rt.Globals.Profile, "rico", &state)
				if state.LastConversationID == "" {
					return fmt.Errorf("no recent conversation to continue — start one with `rc rico`, or pick one with --resume")
				}
				opts.conversationID = state.LastConversationID
			}
			if opts.resume && opts.conversationID == "" {
				opts.conversationID, err = pickRicoConversation(cmd.Context(), rt, client)
				if err != nil {
					return err
				}
			}
			session := &ricoSession{
				rt:             rt,
				client:         client,
				opts:           opts,
				conversationID: opts.conversationID,
				context: rico.DashboardContext{
					ProjectID: rt.Config.ProjectID,
					Pathname:  "/cli",
				},
			}
			if session.conversationID == "" {
				session.conversationID = rico.NewConversationID()
			}
			oneShot := opts.print || !rt.CanPrompt()
			if oneShot || (opts.plain && opts.prompt != "") {
				if opts.prompt == "" {
					return fmt.Errorf("message is required; pass it as an argument or set RC_RICO_PROMPT")
				}
				return session.turn(cmd.Context(), opts.prompt)
			}
			if opts.plain {
				return session.repl(cmd.Context())
			}
			// interactive: a message seeds the chat window, no message opens it empty
			return session.chatWindow(cmd.Context())
		},
	}
	cmd.Flags().StringVar(&opts.prompt, "prompt", opts.prompt, "message to send (or RC_RICO_PROMPT)")
	cmd.Flags().StringVar(&opts.conversationID, "conversation", opts.conversationID, "continue a specific conversation by ID (or RC_RICO_CONVERSATION_ID)")
	cmd.Flags().BoolVarP(&opts.continueLast, "continue", "c", false, "continue the most recent conversation")
	cmd.Flags().BoolVarP(&opts.resume, "resume", "r", false, "pick a past conversation to resume (most recent first)")
	cmd.Flags().BoolVarP(&opts.print, "print", "p", false, "print a single answer and exit (for scripts and agents)")
	cmd.Flags().BoolVar(&opts.approveTools, "approve-tools", false, "approve tool calls without prompting (destructive ones still need --yes)")
	cmd.Flags().BoolVar(&opts.plain, "plain", false, "line-based prompt loop instead of the chat window")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", opts.timeout, "maximum time to wait for a reply")
	cmd.Flags().StringVar(&opts.baseURL, "base-url", opts.baseURL, "Rico endpoint (or RC_RICO_BASE_URL)")
	cmd.AddCommand(
		newRicoConversationsCmd(),
		newRicoFeedbackCmd(),
	)
	return cmd
}

type ricoChatOptions struct {
	prompt         string
	conversationID string
	continueLast   bool
	resume         bool
	print          bool
	approveTools   bool
	plain          bool
	timeout        time.Duration
	baseURL        string
}

// ricoState is the per-profile memory of the last CLI conversation; the
// --resume picker floats it to the top.
type ricoState struct {
	LastConversationID string `json:"last_conversation_id"`
}

// ricoSession drives chat turns: streaming, interrupt approval, and resumes.
type ricoSession struct {
	rt             *Runtime
	client         *rico.Client
	opts           ricoChatOptions
	conversationID string
	context        rico.DashboardContext
}

// ricoSink receives turn progress. The plain/JSON modes print (or stay
// silent); the chat window forwards into the Bubble Tea program.
type ricoSink interface {
	Delta(text string)
	Tool(name string)
	Approve(interrupt rico.Interrupt, label string) (bool, error)
}

// ricoTurnResult is the JSON payload for one completed chat turn.
type ricoTurnResult struct {
	ConversationID string             `json:"conversation_id"`
	RunID          string             `json:"run_id"`
	Reply          string             `json:"reply"`
	ToolCalls      []ricoToolCallInfo `json:"tool_calls,omitempty"`
	Rejected       []ricoToolCallInfo `json:"rejected_tool_calls,omitempty"`
	Status         string             `json:"status"`
}

type ricoToolCallInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Destructive bool   `json:"destructive,omitempty"`
}

// chatWindow opens the full-screen Bubble Tea chat UI.
func (s *ricoSession) chatWindow(ctx context.Context) error {
	transcript := s.loadTranscript(ctx)
	chat := tui.Chat{
		Title:            "Rico",
		Subtitle:         "conversation " + s.conversationID,
		Placeholder:      "Ask Rico anything about your RevenueCat projects…",
		Transcript:       transcript,
		Initial:          s.opts.prompt,
		RelativeLinkBase: envOrDefault("RC_DASHBOARD_URL", "https://app.revenuecat.com"),
		Send: func(turnCtx context.Context, message string, emit *tui.ChatEmitter) {
			turnCtx, cancel := context.WithTimeout(turnCtx, s.opts.timeout)
			defer cancel()
			_, err := s.run(turnCtx, message, &ricoChatUISink{emit: emit})
			if err == nil {
				s.rememberConversation()
			}
			emit.Done(err)
		},
	}
	if err := chat.RunChat(); err != nil {
		return err
	}
	s.rt.Out.Hint("Continue this conversation:  rc rico --conversation " + s.conversationID)
	return nil
}

// pickRicoConversation shows the --resume picker in the alternate screen (so
// nothing lingers on the primary screen once the chat window exits).
func pickRicoConversation(ctx context.Context, rt *Runtime, client *rico.Client) (string, error) {
	if !rt.CanPrompt() {
		return "", fmt.Errorf("conversation ID is required; --resume needs a terminal (use --conversation <id>)")
	}
	items, err := ricoConversationPickerItems(ctx, rt, client)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", fmt.Errorf("no conversations found — start one with rc rico")
	}
	options := make([]huh.Option[string], len(items))
	for i, item := range items {
		options[i] = huh.NewOption(item.Label, item.ID)
	}
	var chosen string
	selectField := huh.NewSelect[string]().
		Title("Resume a conversation").
		Description("Type to filter  ·  Enter to open").
		Options(options...).
		Filtering(true).
		Value(&chosen)
	form := huh.NewForm(huh.NewGroup(selectField)).
		WithTheme(tui.BrandTheme()).
		WithProgramOptions(tea.WithAltScreen())
	if err := form.Run(); err != nil {
		return "", err
	}
	return chosen, nil
}

// ricoConversationPickerItems feeds the --resume picker: the conversation
// from the CLI's last chat floats to the top, the rest keep the server's
// most-recent-first order. Unfiltered by project — conversations span
// projects, and a stale active-project setting must not hide them.
func ricoConversationPickerItems(ctx context.Context, rt *Runtime, client *rico.Client) ([]PickerItem, error) {
	conversations, err := client.ListConversations(ctx, "")
	if err != nil {
		return nil, ricoFriendlyError(err)
	}
	var state ricoState
	_ = config.LoadState(rt.Globals.Profile, "rico", &state)

	// age in a fixed-width gutter so summaries align in a column
	type row struct {
		id, age, summary string
		recent           bool
	}
	rows := make([]row, 0, len(conversations))
	ageWidth := 0
	for _, conversation := range conversations {
		summary := conversation.Summary
		if summary == "" {
			summary = "(no summary)"
		}
		age := lastActivity(conversation.UpdatedAt)
		if len(age) > ageWidth {
			ageWidth = len(age)
		}
		rows = append(rows, row{
			id:      conversation.ID,
			age:     age,
			summary: summary,
			recent:  conversation.ID == state.LastConversationID,
		})
	}

	items := make([]PickerItem, 0, len(rows))
	for _, r := range rows {
		marker := "  "
		if r.recent {
			marker = "↩ "
		}
		item := PickerItem{
			ID:    r.id,
			Label: fmt.Sprintf("%s%-*s   %s", marker, ageWidth, r.age, r.summary),
		}
		if r.recent {
			items = append([]PickerItem{item}, items...)
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

// lastActivity renders an ISO timestamp as a compact relative age ("3h ago");
// older than a week falls back to the date, unparseable input passes through.
func lastActivity(iso string) string {
	t, err := time.Parse(time.RFC3339Nano, iso)
	if err != nil {
		if t, err = time.Parse(time.RFC3339, iso); err != nil {
			return iso
		}
	}
	age := time.Since(t)
	switch {
	case age < time.Minute:
		return "just now"
	case age < time.Hour:
		return fmt.Sprintf("%dm ago", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(age.Hours()))
	case age < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(age.Hours()/24))
	default:
		return t.Local().Format("2006-01-02")
	}
}

// rememberConversation records the conversation so --resume lists it first.
func (s *ricoSession) rememberConversation() {
	_ = config.SaveState(s.rt.Globals.Profile, "rico", ricoState{LastConversationID: s.conversationID})
}

// loadTranscript hydrates prior messages when continuing a conversation.
// Best-effort: a brand-new conversation has no history to fetch.
func (s *ricoSession) loadTranscript(ctx context.Context) []tui.ChatEntry {
	if s.opts.conversationID == "" {
		return nil
	}
	snapshot, err := s.client.GetMessages(ctx, s.conversationID)
	if err != nil {
		return nil
	}
	var entries []tui.ChatEntry
	for _, message := range snapshot.Messages {
		for _, call := range message.ToolCalls {
			entries = append(entries, tui.ChatEntry{Role: tui.ChatTool, Text: call.Function.Name})
		}
		text := message.Text()
		if text == "" {
			continue
		}
		switch message.Role {
		case "user":
			entries = append(entries, tui.ChatEntry{Role: tui.ChatUser, Text: text})
		case "assistant":
			entries = append(entries, tui.ChatEntry{Role: tui.ChatAssistant, Text: text})
		}
	}
	return entries
}

type ricoChatUISink struct {
	emit *tui.ChatEmitter
}

func (s *ricoChatUISink) Delta(text string) { s.emit.Delta(text) }
func (s *ricoChatUISink) Tool(name string)  { s.emit.ToolCall(name) }
func (s *ricoChatUISink) Approve(interrupt rico.Interrupt, label string) (bool, error) {
	return s.emit.Approve(label, interrupt.IsDestructive()), nil
}

func (s *ricoSession) repl(ctx context.Context) error {
	s.rt.Out.Info("Chatting with Rico — empty line or Ctrl-D to exit")
	s.rt.Out.Hint("Conversation " + s.conversationID)
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Fprint(os.Stderr, "\n> ")
		if !scanner.Scan() {
			fmt.Fprintln(os.Stderr)
			return scanner.Err()
		}
		message := strings.TrimSpace(scanner.Text())
		if message == "" {
			return nil
		}
		if err := s.turn(ctx, message); err != nil {
			s.rt.Out.Error(err.Error())
		}
	}
}

// turn runs one message in plain or JSON mode and renders the result.
func (s *ricoSession) turn(ctx context.Context, message string) error {
	ctx, cancel := context.WithTimeout(ctx, s.opts.timeout)
	defer cancel()

	sink := &ricoPlainSink{session: s, silent: s.rt.Out.IsJSON()}
	result, err := s.run(ctx, message, sink)
	sink.endLine()
	if err != nil {
		return err
	}
	s.rememberConversation()
	if s.rt.Out.IsJSON() {
		return s.rt.Out.Render(result)
	}
	s.rt.Out.Hint("Conversation " + s.conversationID + " · run " + result.RunID)
	return nil
}

// run executes one user turn end-to-end, looping through interrupt/approval
// rounds until the run finishes.
func (s *ricoSession) run(ctx context.Context, message string, sink ricoSink) (*ricoTurnResult, error) {
	runID := rico.NewRunID()
	result := &ricoTurnResult{ConversationID: s.conversationID, RunID: runID}
	input := rico.ChatInput(s.conversationID, runID, message, s.context, nil)
	for round := 0; ; round++ {
		interrupts, err := s.streamRun(ctx, input, result, sink)
		if err != nil {
			return nil, err
		}
		if len(interrupts) == 0 {
			return result, nil
		}
		if round >= ricoResumeRounds {
			return nil, fmt.Errorf("giving up after %d tool-approval rounds in one turn", ricoResumeRounds)
		}
		resume, err := s.resolveInterrupts(interrupts, result, sink)
		if err != nil {
			return nil, err
		}
		runID = rico.NewRunID()
		result.RunID = runID
		input = rico.ResumeInput(s.conversationID, runID, s.context, resume)
	}
}

// streamRun executes one POST /v1/agent stream and returns any interrupts
// that paused the run.
func (s *ricoSession) streamRun(ctx context.Context, input rico.RunAgentInput, result *ricoTurnResult, sink ricoSink) ([]rico.Interrupt, error) {
	stream, err := s.client.Stream(ctx, input)
	if err != nil {
		return nil, ricoFriendlyError(err)
	}
	defer stream.Close()

	for {
		event, err := stream.Next()
		if err == io.EOF {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		switch event.Type {
		case rico.EventTextMessageStart:
			// Separate consecutive assistant messages; the stream carries no
			// whitespace between them.
			if result.Reply != "" && !strings.HasSuffix(result.Reply, "\n") {
				result.Reply += "\n\n"
				sink.Delta("\n\n")
			}
		case rico.EventTextMessageContent:
			result.Reply += event.Delta
			sink.Delta(event.Delta)
		case rico.EventToolCallStart:
			result.ToolCalls = append(result.ToolCalls, ricoToolCallInfo{ID: event.ToolCallID, Name: event.ToolCallName})
			sink.Tool(event.ToolCallName)
		case rico.EventRunError:
			result.Status = "error"
			return nil, fmt.Errorf("rico run failed: %s", event.Message)
		case rico.EventRunFinished:
			if event.Outcome != nil && event.Outcome.Type == "interrupt" {
				result.Status = "interrupted"
				return event.Outcome.Interrupts, nil
			}
			result.Status = "success"
			return nil, nil
		}
	}
}

// resolveInterrupts turns pending interrupts into resume entries via the
// sink's approval flow.
func (s *ricoSession) resolveInterrupts(interrupts []rico.Interrupt, result *ricoTurnResult, sink ricoSink) ([]rico.ResumeEntry, error) {
	entries := make([]rico.ResumeEntry, 0, len(interrupts))
	for _, interrupt := range interrupts {
		label := interrupt.Message
		if label == "" {
			label = interrupt.Reason
		}
		approved, err := sink.Approve(interrupt, label)
		if err != nil {
			return nil, err
		}
		if approved {
			entries = append(entries, rico.ApproveInterrupt(interrupt))
			continue
		}
		entries = append(entries, rico.RejectInterrupt(interrupt))
		result.Rejected = append(result.Rejected, ricoToolCallInfo{
			ID:          interrupt.ResumeID(),
			Reason:      interrupt.Reason,
			Destructive: interrupt.IsDestructive(),
		})
	}
	return entries, nil
}

// ricoPlainSink prints streaming output to stdout (unless silent, for --json)
// and applies the prompt/--approve-tools approval policy.
type ricoPlainSink struct {
	session *ricoSession
	silent  bool
	midLine bool
}

func (s *ricoPlainSink) Delta(text string) {
	if s.silent {
		return
	}
	fmt.Print(text)
	s.midLine = true
}

func (s *ricoPlainSink) endLine() {
	if s.midLine {
		fmt.Println()
		s.midLine = false
	}
}

func (s *ricoPlainSink) Tool(name string) {
	if s.silent {
		return
	}
	s.endLine()
	s.session.rt.Out.Info("⚙ " + name)
}

func (s *ricoPlainSink) Approve(interrupt rico.Interrupt, label string) (bool, error) {
	s.endLine()
	rt := s.session.rt
	if interrupt.IsDestructive() {
		label += " (destructive)"
	}
	if !rt.CanPrompt() {
		approved := s.session.opts.approveTools && (!interrupt.IsDestructive() || rt.Globals.AssumeYes)
		if !approved && !s.silent {
			rt.Out.Warn("Rejected tool call: " + label)
		}
		return approved, nil
	}
	return tui.ConfirmDefault(rt.Globals.NoInput, "Allow Rico to run: "+label+"?", !interrupt.IsDestructive())
}

func newRicoConversationsCmd() *cobra.Command {
	baseURL := devEnvOrDefault("RC_RICO_BASE_URL", rico.DefaultBaseURL)
	cmd := &cobra.Command{
		Use:     "conversations",
		Aliases: []string{"conversation"},
		Short:   "List, inspect, and delete Rico conversations",
	}

	var listProjectID string
	list := &cobra.Command{
		Use:   "list",
		Short: "List conversations",
		Long: `Lists all Rico conversations for the authenticated account. Pass --project to
scope to a single Project.`,
		Example: `  rc rico conversations list
  rc rico conversations list --project proj_x`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			client, err := ricoClient(rt, baseURL)
			if err != nil {
				return err
			}
			conversations, err := client.ListConversations(cmd.Context(), listProjectID)
			if err != nil {
				return ricoFriendlyError(err)
			}
			rows := make([][]string, 0, len(conversations))
			for _, c := range conversations {
				summary := c.Summary
				if summary == "" {
					summary = "—"
				}
				rows = append(rows, []string{c.ID, summary, lastActivity(c.UpdatedAt)})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "SUMMARY", "UPDATED"},
				Rows:    rows,
				Raw:     conversations,
			})
		},
	}

	show := &cobra.Command{
		Use:     "show <id>",
		Short:   "Show a conversation transcript",
		Long:    `Prints the full message transcript for a Rico conversation, including tool calls and any pending approvals.`,
		Example: `  rc rico conversations show NQ7bGmww8rLcPT9d`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			client, err := ricoClient(rt, baseURL)
			if err != nil {
				return err
			}
			snapshot, err := client.GetMessages(cmd.Context(), args[0])
			if err != nil {
				return ricoFriendlyError(err)
			}
			if rt.Out.IsJSON() {
				return rt.Out.Render(snapshot)
			}
			for _, message := range snapshot.Messages {
				text := message.Text()
				for _, call := range message.ToolCalls {
					rt.Out.Info("⚙ " + call.Function.Name)
				}
				if text == "" {
					continue
				}
				fmt.Printf("%s: %s\n", message.Role, text)
			}
			for _, interrupt := range snapshot.PendingInterrupts {
				rt.Out.Warn("Pending approval: " + interrupt.Reason)
			}
			return nil
		},
	}

	del := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a conversation",
		Long: `Permanently deletes a conversation and its history.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			if err := confirmOrAbort(rt, fmt.Sprintf("Delete conversation %q?", args[0])); err != nil {
				return err
			}
			client, err := ricoClient(rt, baseURL)
			if err != nil {
				return err
			}
			if err := client.DeleteConversation(cmd.Context(), args[0]); err != nil {
				return ricoFriendlyError(err)
			}
			rt.Out.Success("Deleted " + args[0])
			return rt.Out.Render(map[string]any{"ok": true, "id": args[0]})
		},
	}

	list.Flags().StringVar(&listProjectID, "project", "", "only conversations touching this project")
	cmd.AddCommand(list, show, del)
	cmd.PersistentFlags().StringVar(&baseURL, "base-url", baseURL, "Rico endpoint (or RC_RICO_BASE_URL)")
	return cmd
}

func newRicoFeedbackCmd() *cobra.Command {
	var comment string
	baseURL := devEnvOrDefault("RC_RICO_BASE_URL", rico.DefaultBaseURL)
	cmd := &cobra.Command{
		Use:   "feedback <run-id> <good|bad>",
		Short: "Rate a Rico reply",
		Long:  `Rates a Rico reply good or bad by run ID (printed after each chat turn), with an optional comment.`,
		Example: `  rc rico feedback rico_cli_1a2b3c good
  rc rico feedback rico_cli_1a2b3c bad --comment "wrong project"`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			var score float64
			switch args[1] {
			case "good":
				score = 1
			case "bad":
				score = 0
			default:
				return fmt.Errorf("rating must be \"good\" or \"bad\", got %q", args[1])
			}
			client, err := ricoClient(rt, baseURL)
			if err != nil {
				return err
			}
			if err := client.PostFeedback(cmd.Context(), rico.FeedbackRequest{RunID: args[0], Score: score, Comment: comment}); err != nil {
				return ricoFriendlyError(err)
			}
			rt.Out.Success("Feedback sent")
			return rt.Out.Render(map[string]any{"ok": true, "run_id": args[0], "score": score})
		},
	}
	cmd.Flags().StringVar(&comment, "comment", "", "optional comment")
	cmd.Flags().StringVar(&baseURL, "base-url", baseURL, "Rico endpoint (or RC_RICO_BASE_URL)")
	return cmd
}

func ricoClient(rt *Runtime, baseURL string) (*rico.Client, error) {
	// rt.API() refreshes the OAuth token if needed and enforces login.
	if _, err := rt.API(); err != nil {
		return nil, err
	}
	return rico.NewClient(rico.Options{
		BaseURL:      baseURL,
		Token:        agentAuthToken(rt),
		UserAgent:    userAgent(rt.Globals.Version),
		ExtraHeaders: customHeaders(),
	}), nil
}

// agentAuthToken returns the credential sent to the Rico/Paywalls AI backends —
// the CLI's own bearer token; both backends accept CLI OAuth tokens
// (verified live 2026-07-17).
func agentAuthToken(rt *Runtime) string {
	return rt.Config.BearerToken()
}

func ricoFriendlyError(err error) error {
	var apiErr *rico.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.StatusCode {
	case 401, 403:
		return fmt.Errorf("%w; Rico may not be enabled for this account (check the project's AI settings)", err)
	case 429:
		if apiErr.RetryAfter > 0 {
			return fmt.Errorf("%w; retry in %s", err, apiErr.RetryAfter)
		}
	}
	return err
}
