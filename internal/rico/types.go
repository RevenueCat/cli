// Package rico is a client for the Rico agent backend (rico.revenuecat.com),
// speaking the same AG-UI protocol as the dashboard and mobile apps.
package rico

import (
	"encoding/json"
	"strings"
)

// AG-UI event type codes as they appear on the wire.
const (
	EventRunStarted              = "RUN_STARTED"
	EventRunFinished             = "RUN_FINISHED"
	EventRunError                = "RUN_ERROR"
	EventTextMessageStart        = "TEXT_MESSAGE_START"
	EventTextMessageContent      = "TEXT_MESSAGE_CONTENT"
	EventTextMessageEnd          = "TEXT_MESSAGE_END"
	EventReasoningMessageStart   = "REASONING_MESSAGE_START"
	EventReasoningMessageContent = "REASONING_MESSAGE_CONTENT"
	EventReasoningMessageEnd     = "REASONING_MESSAGE_END"
	EventToolCallStart           = "TOOL_CALL_START"
	EventToolCallArgs            = "TOOL_CALL_ARGS"
	EventToolCallEnd             = "TOOL_CALL_END"
	EventCustom                  = "CUSTOM"
	EventMessagesSnapshot        = "MESSAGES_SNAPSHOT"
)

// Event is one AG-UI stream event. Fields are populated according to Type;
// unrecognized event types are preserved via Raw.
type Event struct {
	Type              string             `json:"type"`
	ThreadID          string             `json:"threadId,omitempty"`
	RunID             string             `json:"runId,omitempty"`
	MessageID         string             `json:"messageId,omitempty"`
	Role              string             `json:"role,omitempty"`
	Delta             string             `json:"delta,omitempty"`
	ToolCallID        string             `json:"toolCallId,omitempty"`
	ToolCallName      string             `json:"toolCallName,omitempty"`
	Message           string             `json:"message,omitempty"` // RUN_ERROR
	Code              string             `json:"code,omitempty"`    // RUN_ERROR
	Outcome           *RunOutcome        `json:"outcome,omitempty"` // RUN_FINISHED
	Name              string             `json:"name,omitempty"`    // CUSTOM
	Value             json.RawMessage    `json:"value,omitempty"`   // CUSTOM
	Messages          []Message          `json:"messages,omitempty"`
	PendingInterrupts []Interrupt        `json:"pendingInterrupts,omitempty"`
	ResolvedApprovals []ResolvedApproval `json:"resolvedApprovals,omitempty"`
	Timestamp         int64              `json:"timestamp,omitempty"`

	Raw json.RawMessage `json:"-"`
}

// RunOutcome reports how a RUN_FINISHED run ended: "success", or "interrupt"
// with the interrupts that paused it.
type RunOutcome struct {
	Type       string      `json:"type"`
	Interrupts []Interrupt `json:"interrupts,omitempty"`
}

type Interrupt struct {
	ID         string             `json:"id"`
	Reason     string             `json:"reason,omitempty"`
	ToolCallID string             `json:"toolCallId,omitempty"`
	Message    string             `json:"message,omitempty"`
	Metadata   *InterruptMetadata `json:"metadata,omitempty"`
	ExpiresAt  string             `json:"expiresAt,omitempty"`
}

// ResumeID is the identifier expected in a ResumeEntry: the tool call the
// interrupt refers to, falling back to the interrupt's own ID.
func (i Interrupt) ResumeID() string {
	if i.ToolCallID != "" {
		return i.ToolCallID
	}
	return i.ID
}

func (i Interrupt) IsDestructive() bool {
	return i.Metadata != nil && i.Metadata.IsDestructive
}

type InterruptMetadata struct {
	IsDestructive bool `json:"isDestructive"`
}

func (m *InterruptMetadata) UnmarshalJSON(data []byte) error {
	var wire struct {
		Camel *bool `json:"isDestructive"`
		Snake *bool `json:"is_destructive"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Camel != nil {
		m.IsDestructive = *wire.Camel
	} else if wire.Snake != nil {
		m.IsDestructive = *wire.Snake
	}
	return nil
}

type ResolvedApproval struct {
	InterruptID string `json:"interruptId"`
	Decision    string `json:"decision"` // approved | rejected | dismissed
}

type ResumeEntry struct {
	InterruptID string         `json:"interruptId"`
	Status      string         `json:"status"` // resolved | cancelled
	Payload     *ResumePayload `json:"payload,omitempty"`
}

type ResumePayload struct {
	Approved bool `json:"approved"`
}

func ApproveInterrupt(i Interrupt) ResumeEntry {
	return ResumeEntry{InterruptID: i.ResumeID(), Status: "resolved", Payload: &ResumePayload{Approved: true}}
}

func RejectInterrupt(i Interrupt) ResumeEntry {
	return ResumeEntry{InterruptID: i.ResumeID(), Status: "resolved", Payload: &ResumePayload{Approved: false}}
}

// Message is one transcript entry from a snapshot.
type Message struct {
	ID        string          `json:"id"`
	Role      string          `json:"role"`
	Content   *MessageContent `json:"content,omitempty"`
	ToolCalls []ToolCall      `json:"toolCalls,omitempty"`
	RunID     string          `json:"runId,omitempty"`
}

func (m Message) Text() string {
	if m.Content == nil {
		return ""
	}
	return m.Content.Text()
}

// MessageContent is either a plain string or a list of typed parts.
type MessageContent struct {
	Plain string
	Parts []ContentPart
}

type ContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

func (c *MessageContent) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &c.Plain)
	}
	return json.Unmarshal(data, &c.Parts)
}

func (c MessageContent) MarshalJSON() ([]byte, error) {
	if c.Parts != nil {
		return json.Marshal(c.Parts)
	}
	return json.Marshal(c.Plain)
}

func (c MessageContent) Text() string {
	if c.Parts == nil {
		return c.Plain
	}
	var parts []string
	for _, p := range c.Parts {
		if p.Type == "text" && p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// MessagesSnapshot is the response of GET /v1/conversations/{id}/messages and
// the payload of MESSAGES_SNAPSHOT events.
type MessagesSnapshot struct {
	Messages          []Message          `json:"messages"`
	PendingInterrupts []Interrupt        `json:"pendingInterrupts,omitempty"`
	ResolvedApprovals []ResolvedApproval `json:"resolvedApprovals,omitempty"`
}

type Conversation struct {
	ID        string `json:"id"`
	Summary   string `json:"summary,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// RunAgentInput is the AG-UI request body for POST /v1/agent.
type RunAgentInput struct {
	ThreadID       string         `json:"threadId"`
	RunID          string         `json:"runId"`
	State          any            `json:"state"`
	Messages       []UserMessage  `json:"messages"`
	Tools          []struct{}     `json:"tools"`
	Context        []struct{}     `json:"context"`
	ForwardedProps ForwardedProps `json:"forwardedProps"`
	Resume         []ResumeEntry  `json:"resume,omitempty"`
}

type UserMessage struct {
	ID      string         `json:"id"`
	Role    string         `json:"role"`
	Content MessageContent `json:"content"`
}

type ForwardedProps struct {
	DashboardContext DashboardContext `json:"dashboard_context"`
}

// DashboardContext tells Rico where the user is. The CLI reports a synthetic
// pathname so responses can be scoped like dashboard pages are.
type DashboardContext struct {
	ProjectID string `json:"project_id,omitempty"`
	Pathname  string `json:"pathname"`
}

// ChatInput builds the RunAgentInput for a new user turn, mirroring the
// first-party clients: one user message whose ID is derived from the run ID,
// empty tools/context, and a null state.
func ChatInput(conversationID, runID, message string, context DashboardContext, resume []ResumeEntry) RunAgentInput {
	input := ResumeInput(conversationID, runID, context, resume)
	input.Messages = []UserMessage{{
		ID:      "user_" + runID,
		Role:    "user",
		Content: MessageContent{Plain: message},
	}}
	return input
}

// ResumeInput builds a RunAgentInput that only answers pending interrupts.
func ResumeInput(conversationID, runID string, context DashboardContext, resume []ResumeEntry) RunAgentInput {
	return RunAgentInput{
		ThreadID:       conversationID,
		RunID:          runID,
		Messages:       []UserMessage{},
		Tools:          []struct{}{},
		Context:        []struct{}{},
		ForwardedProps: ForwardedProps{DashboardContext: context},
		Resume:         resume,
	}
}
