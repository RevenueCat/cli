// Package astra is a client for the Paywall AI editor backend
// (astra.revenuecat.com), speaking the same SSE contract as the dashboard
// and mobile apps.
package astra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/revenuecat/cli/internal/sse"
)

const DefaultBaseURL = "https://astra.revenuecat.com"

// Stream event type codes.
const (
	EventRunStarted   = "run.started"
	EventTurnSnapshot = "turn.snapshot"
	EventRunCompleted = "run.completed"
	EventRunFailed    = "run.failed"
)

// PaywallData is the paywall state round-tripped with the editor each turn.
// Components config and localizations are opaque to the CLI.
type PaywallData struct {
	DefaultLocale           string          `json:"default_locale"`
	OfferingID              *string         `json:"offering_id"`
	ComponentsConfig        json.RawMessage `json:"components_config"`
	ComponentsLocalizations json.RawMessage `json:"components_localizations"`
}

type InputAttachment struct {
	Type       string `json:"type"` // "image"
	Filename   string `json:"filename,omitempty"`
	MimeType   string `json:"mime_type"`
	DataBase64 string `json:"data_base64"`
}

// EditorRequest is the POST /editor/v1/stream body. UIConfig, SessionItems,
// and AppContext are opaque server round-trips.
type EditorRequest struct {
	ProjectID        string            `json:"project_id"`
	PaywallID        string            `json:"paywall_id"`
	Revision         *int              `json:"revision"`
	SessionID        string            `json:"session_id,omitempty"`
	Paywall          PaywallData       `json:"paywall"`
	UIConfig         json.RawMessage   `json:"ui_config"`
	ProductVariables map[string]string `json:"product_variables"`
	Message          string            `json:"message"`
	InputAttachments []InputAttachment `json:"input_attachments,omitempty"`
	SessionItems     json.RawMessage   `json:"__unstable_session_items"`
	AppContext       json.RawMessage   `json:"app_context,omitempty"`
}

type ToolActivity struct {
	ID         string `json:"id"`
	Type       string `json:"type"` // "tool" | "assistant_message"
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	Status     string `json:"status,omitempty"` // success | error
	Display    struct {
		Text string `json:"text"`
	} `json:"display,omitempty"`
	Content string `json:"content,omitempty"` // assistant_message
}

type StreamError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Event is one editor stream event; fields are populated per Type.
type Event struct {
	Type         string          `json:"type"`
	SessionID    string          `json:"session_id"`
	TurnIndex    int             `json:"turn_index,omitempty"`
	TraceID      string          `json:"trace_id,omitempty"`
	Paywall      *PaywallData    `json:"paywall,omitempty"`
	Activity     []ToolActivity  `json:"activity,omitempty"`
	Error        *StreamError    `json:"error,omitempty"`
	SessionItems json.RawMessage `json:"__unstable_session_items,omitempty"`
	AppContext   json.RawMessage `json:"app_context,omitempty"`
}

func (e *Event) Terminal() bool {
	return e.Type == EventRunCompleted || e.Type == EventRunFailed
}

type Client struct {
	baseURL   string
	token     string
	userAgent string
	rest      *http.Client
	stream    *http.Client
}

type Options struct {
	BaseURL   string
	Token     string
	UserAgent string
	// HTTPClient overrides both REST and streaming transports (tests).
	HTTPClient *http.Client
}

func NewClient(opts Options) *Client {
	baseURL := strings.TrimSuffix(opts.BaseURL, "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	rest := opts.HTTPClient
	if rest == nil {
		rest = &http.Client{Timeout: 30 * time.Second}
	}
	stream := opts.HTTPClient
	if stream == nil {
		stream = &http.Client{} // editor runs stream for minutes; ctx cancels
	}
	return &Client{
		baseURL:   baseURL,
		token:     opts.Token,
		userAgent: opts.UserAgent,
		rest:      rest,
		stream:    stream,
	}
}

type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("the Paywall AI editor returned HTTP %d: %s", e.StatusCode, e.Message)
}

func (c *Client) newRequest(ctx context.Context, path string, body any) (*http.Request, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	// Bearer only: the Paywall AI editor accepts CLI OAuth tokens in the Authorization
	// header. Do not also send an rc_auth_token cookie — an invalid cookie
	// shadows a valid bearer token and fails the whole request.
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	return req, nil
}

func checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return &APIError{StatusCode: resp.StatusCode, Message: errorMessage(body)}
}

func errorMessage(body []byte) string {
	var envelope struct {
		Detail  string `json:"detail"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		if envelope.Detail != "" {
			return envelope.Detail
		}
		if envelope.Message != "" {
			return envelope.Message
		}
	}
	if trimmed := strings.TrimSpace(string(body)); trimmed != "" {
		return trimmed
	}
	return "no response body"
}

// Stream starts an editor run and returns the live event stream.
func (c *Client) Stream(ctx context.Context, request EditorRequest) (*Stream, error) {
	req, err := c.newRequest(ctx, "/editor/v1/stream", request)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.stream.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling the Paywall AI editor: %w", err)
	}
	if err := checkStatus(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	return &Stream{body: resp.Body, reader: sse.NewReader(resp.Body)}, nil
}

func (c *Client) doJSON(ctx context.Context, path string, body any) error {
	req, err := c.newRequest(ctx, path, body)
	if err != nil {
		return err
	}
	resp, err := c.rest.Do(req)
	if err != nil {
		return fmt.Errorf("calling the Paywall AI editor: %w", err)
	}
	defer resp.Body.Close()
	return checkStatus(resp)
}

// Feedback rates a completed run ("good" or "bad").
func (c *Client) Feedback(ctx context.Context, sessionID, traceID, rating string) error {
	return c.doJSON(ctx, "/editor/v1/feedback", map[string]string{
		"session_id": sessionID,
		"trace_id":   traceID,
		"rating":     rating,
	})
}

// Rewind undoes the last editor action in a session.
func (c *Client) Rewind(ctx context.Context, sessionID, traceID string, isLastMessage bool) error {
	return c.doJSON(ctx, "/editor/v1/rewind", map[string]any{
		"session_id":      sessionID,
		"trace_id":        traceID,
		"is_last_message": isLastMessage,
	})
}

// Stream yields decoded editor events. Keepalive comments are skipped.
type Stream struct {
	body   io.ReadCloser
	reader *sse.Reader
}

// Next returns the next event, or io.EOF when the server closes the stream.
func (s *Stream) Next() (*Event, error) {
	for {
		frame, err := s.reader.Next()
		if err != nil {
			return nil, err
		}
		if frame.Comment || frame.Data == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(frame.Data), &event); err != nil {
			return nil, fmt.Errorf("decoding Paywall AI editor event: %w", err)
		}
		return &event, nil
	}
}

func (s *Stream) Close() error {
	return s.body.Close()
}
