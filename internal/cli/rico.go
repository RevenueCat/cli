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

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/rico"
	"github.com/revenuecat/cli/internal/tui"
)

// ricoResumeRounds caps interrupt-approval loops within one chat turn so a
// misbehaving run can't hold the CLI forever.
const ricoResumeRounds = 10

func newRicoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rico",
		Short: "Chat with Rico, RevenueCat's AI assistant",
		Long: `Rico answers questions about your projects, metrics, and configuration, and
can run RevenueCat tools on your behalf. Tool calls that change data pause for
approval before executing.

Conversations are stored server-side; continue one with --conversation.`,
	}
	cmd.AddCommand(
		newRicoChatCmd(),
		newRicoConversationsCmd(),
		newRicoFeedbackCmd(),
	)
	return cmd
}

type ricoChatOptions struct {
	prompt         string
	conversationID string
	approveTools   bool
	timeout        time.Duration
	baseURL        string
}

func newRicoChatCmd() *cobra.Command {
	opts := ricoChatOptions{
		prompt:         os.Getenv("RC_RICO_PROMPT"),
		conversationID: os.Getenv("RC_RICO_CONVERSATION_ID"),
		baseURL:        envOrDefault("RC_RICO_BASE_URL", rico.DefaultBaseURL),
		timeout:        10 * time.Minute,
	}
	cmd := &cobra.Command{
		Use:   "chat [message]",
		Short: "Send a message to Rico",
		Long: `Sends a message and streams Rico's reply. In a terminal with no message
given, opens an interactive chat; with a message (or --prompt) it answers once
and exits.

Tool calls that modify data pause the run for approval. Interactive terminals
prompt; non-interactive runs reject them unless --approve-tools is passed
(destructive tools additionally require --yes).`,
		Example: `  rc rico chat
  rc rico chat "why did trial conversions drop this week?"
  rc rico chat "delete the test offering" --approve-tools --yes --json --no-input
  rc rico chat "continue" --conversation NQ7bGmww8rLcPT9d`,
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
			if opts.prompt != "" || rt.Globals.NoInput || !tui.IsInteractive() {
				if opts.prompt == "" {
					return fmt.Errorf("message is required; pass it as an argument or set RC_RICO_PROMPT")
				}
				return session.turn(cmd.Context(), opts.prompt)
			}
			return session.repl(cmd.Context())
		},
	}
	cmd.Flags().StringVar(&opts.prompt, "prompt", opts.prompt, "message to send (or RC_RICO_PROMPT)")
	cmd.Flags().StringVarP(&opts.conversationID, "conversation", "c", opts.conversationID, "conversation to continue (or RC_RICO_CONVERSATION_ID)")
	cmd.Flags().BoolVar(&opts.approveTools, "approve-tools", false, "approve tool calls without prompting (destructive ones still need --yes)")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", opts.timeout, "maximum time to wait for a reply")
	cmd.Flags().StringVar(&opts.baseURL, "base-url", opts.baseURL, "Rico endpoint (or RC_RICO_BASE_URL)")
	return cmd
}

// ricoSession drives chat turns: streaming, interrupt approval, and resumes.
type ricoSession struct {
	rt             *Runtime
	client         *rico.Client
	opts           ricoChatOptions
	conversationID string
	context        rico.DashboardContext
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

func (s *ricoSession) repl(ctx context.Context) error {
	s.rt.Out.Info("Chatting with Rico — empty line or Ctrl-D to exit")
	s.rt.Out.Info("Conversation: " + s.conversationID)
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

// turn sends one user message and streams until the run completes, handling
// interrupt/approval rounds along the way.
func (s *ricoSession) turn(ctx context.Context, message string) error {
	ctx, cancel := context.WithTimeout(ctx, s.opts.timeout)
	defer cancel()

	runID := rico.NewRunID()
	result := &ricoTurnResult{ConversationID: s.conversationID, RunID: runID}
	input := rico.ChatInput(s.conversationID, runID, message, s.context, nil)
	for round := 0; ; round++ {
		interrupts, err := s.streamRun(ctx, input, result)
		if err != nil {
			return err
		}
		if len(interrupts) == 0 {
			break
		}
		if round >= ricoResumeRounds {
			return fmt.Errorf("giving up after %d tool-approval rounds in one turn", ricoResumeRounds)
		}
		resume, err := s.resolveInterrupts(interrupts, result)
		if err != nil {
			return err
		}
		runID = rico.NewRunID()
		result.RunID = runID
		input = rico.ResumeInput(s.conversationID, runID, s.context, resume)
	}
	if s.rt.Out.IsJSON() {
		return s.rt.Out.Render(result)
	}
	s.rt.Out.Info("Conversation " + s.conversationID + " · run " + result.RunID)
	return nil
}

// streamRun executes one POST /v1/agent stream and returns any interrupts
// that paused the run.
func (s *ricoSession) streamRun(ctx context.Context, input rico.RunAgentInput, result *ricoTurnResult) ([]rico.Interrupt, error) {
	stream, err := s.client.Stream(ctx, input)
	if err != nil {
		return nil, ricoFriendlyError(err)
	}
	defer stream.Close()

	jsonMode := s.rt.Out.IsJSON()
	printedText := false
	for {
		event, err := stream.Next()
		if err == io.EOF {
			if printedText {
				fmt.Println()
			}
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		switch event.Type {
		case rico.EventTextMessageContent:
			result.Reply += event.Delta
			if !jsonMode {
				fmt.Print(event.Delta)
				printedText = true
			}
		case rico.EventToolCallStart:
			result.ToolCalls = append(result.ToolCalls, ricoToolCallInfo{ID: event.ToolCallID, Name: event.ToolCallName})
			if !jsonMode {
				if printedText {
					fmt.Println()
					printedText = false
				}
				s.rt.Out.Info("⚙ " + event.ToolCallName)
			}
		case rico.EventRunError:
			if printedText {
				fmt.Println()
			}
			result.Status = "error"
			return nil, fmt.Errorf("Rico run failed: %s", event.Message)
		case rico.EventRunFinished:
			if printedText {
				fmt.Println()
			}
			if event.Outcome != nil && event.Outcome.Type == "interrupt" {
				result.Status = "interrupted"
				return event.Outcome.Interrupts, nil
			}
			result.Status = "success"
			return nil, nil
		}
	}
}

// resolveInterrupts turns pending interrupts into resume entries, prompting
// the user (TTY) or applying the --approve-tools / --yes policy.
func (s *ricoSession) resolveInterrupts(interrupts []rico.Interrupt, result *ricoTurnResult) ([]rico.ResumeEntry, error) {
	entries := make([]rico.ResumeEntry, 0, len(interrupts))
	for _, interrupt := range interrupts {
		label := interrupt.Message
		if label == "" {
			label = interrupt.Reason
		}
		if interrupt.IsDestructive() {
			label += " (destructive)"
		}
		approved, err := s.approves(interrupt, label)
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
		s.rt.Out.Warn("Rejected tool call: " + label)
	}
	return entries, nil
}

func (s *ricoSession) approves(interrupt rico.Interrupt, label string) (bool, error) {
	if s.rt.Globals.NoInput || !tui.IsInteractive() {
		if !s.opts.approveTools {
			return false, nil
		}
		if interrupt.IsDestructive() && !s.rt.Globals.AssumeYes {
			return false, nil
		}
		return true, nil
	}
	return tui.ConfirmDefault(s.rt.Globals.NoInput, "Allow Rico to run: "+label+"?", !interrupt.IsDestructive())
}

func newRicoConversationsCmd() *cobra.Command {
	baseURL := envOrDefault("RC_RICO_BASE_URL", rico.DefaultBaseURL)
	cmd := &cobra.Command{
		Use:     "conversations",
		Aliases: []string{"conversation"},
		Short:   "List, inspect, and delete Rico conversations",
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "List conversations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			client, err := ricoClient(rt, baseURL)
			if err != nil {
				return err
			}
			conversations, err := client.ListConversations(cmd.Context(), rt.Config.ProjectID)
			if err != nil {
				return ricoFriendlyError(err)
			}
			rows := make([][]string, 0, len(conversations))
			for _, c := range conversations {
				summary := c.Summary
				if summary == "" {
					summary = "—"
				}
				rows = append(rows, []string{c.ID, summary, c.UpdatedAt})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "SUMMARY", "UPDATED"},
				Rows:    rows,
				Raw:     conversations,
			})
		},
	}

	show := &cobra.Command{
		Use:   "show <id>",
		Short: "Show a conversation transcript",
		Args:  cobra.ExactArgs(1),
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
			if !rt.Globals.AssumeYes {
				ok, err := tui.Confirm(rt.Globals.NoInput, fmt.Sprintf("Delete conversation %q?", args[0]))
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("aborted")
				}
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

	cmd.AddCommand(list, show, del)
	cmd.PersistentFlags().StringVar(&baseURL, "base-url", baseURL, "Rico endpoint (or RC_RICO_BASE_URL)")
	return cmd
}

func newRicoFeedbackCmd() *cobra.Command {
	var comment string
	baseURL := envOrDefault("RC_RICO_BASE_URL", rico.DefaultBaseURL)
	cmd := &cobra.Command{
		Use:   "feedback <run-id> <good|bad>",
		Short: "Rate a Rico reply",
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
		BaseURL:   baseURL,
		Token:     rt.Config.BearerToken(),
		UserAgent: userAgent(rt.Globals.Version),
	}), nil
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

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
