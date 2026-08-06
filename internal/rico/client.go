package rico

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/revenuecat/cli/internal/httpx"
	"github.com/revenuecat/cli/internal/sse"
)

const DefaultBaseURL = "https://rico.revenuecat.com"

// conversationAlphabet matches the nanoid alphabet used by the dashboard and
// iOS clients for conversation IDs.
const conversationAlphabet = "6789BCDFGHJKLMNPQRTWbcdfghjkmnpqrtwz"

type Client struct {
	baseURL      string
	token        string
	userAgent    string
	extraHeaders http.Header
	rest         *http.Client
	stream       *http.Client
}

type Options struct {
	BaseURL   string
	Token     string
	UserAgent string
	// HTTPClient overrides both REST and streaming transports (tests).
	HTTPClient *http.Client
	// ExtraHeaders are sent on every request (from RC_HEADERS).
	ExtraHeaders http.Header
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
		// No client timeout: agent runs stream for minutes. Cancellation is the
		// caller's context; keepalive comments guard against dead connections.
		stream = &http.Client{}
	}
	return &Client{
		baseURL:      baseURL,
		token:        opts.Token,
		userAgent:    opts.UserAgent,
		extraHeaders: opts.ExtraHeaders,
		rest:         rest,
		stream:       stream,
	}
}

// APIError is a non-2xx response from the Rico backend.
type APIError struct {
	StatusCode int
	Message    string
	RetryAfter time.Duration // set on 429
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Rico returned HTTP %d: %s", e.StatusCode, e.Message)
}

func NewConversationID() string {
	return randomString(16, conversationAlphabet)
}

func NewRunID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	return "rico_cli_" + hex.EncodeToString(buf)
}

func randomString(n int, alphabet string) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	out := make([]byte, n)
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out)
}

func (c *Client) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	httpx.Apply(req, c.extraHeaders)
	return req, nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	resp, err := c.rest.Do(req)
	if err != nil {
		return fmt.Errorf("calling Rico: %w", err)
	}
	defer resp.Body.Close()
	if err := checkStatus(resp); err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 25<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decoding Rico response: %w", err)
	}
	return nil
}

func checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	apiErr := &APIError{StatusCode: resp.StatusCode, Message: errorMessage(body)}
	if resp.StatusCode == http.StatusTooManyRequests {
		if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			apiErr.RetryAfter = time.Duration(seconds) * time.Second
		}
	}
	return apiErr
}

func errorMessage(body []byte) string {
	var envelope struct {
		Detail  json.RawMessage `json:"detail"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		if envelope.Message != "" {
			return envelope.Message
		}
		var detail string
		if json.Unmarshal(envelope.Detail, &detail) == nil && detail != "" {
			return detail
		}
		if len(envelope.Detail) > 0 {
			return string(envelope.Detail)
		}
	}
	if trimmed := strings.TrimSpace(string(body)); trimmed != "" {
		return trimmed
	}
	return "no response body"
}

// CheckCapabilities verifies this client is still accepted by the backend.
func (c *Client) CheckCapabilities(ctx context.Context) error {
	return c.doJSON(ctx, http.MethodGet, "/v1/agent/capabilities", nil, nil)
}

func (c *Client) ListConversations(ctx context.Context, projectID string) ([]Conversation, error) {
	path := "/v1/conversations"
	if projectID != "" {
		path += "?project_id=" + url.QueryEscape(projectID)
	}
	var out []Conversation
	err := c.doJSON(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

func (c *Client) GetMessages(ctx context.Context, conversationID string) (*MessagesSnapshot, error) {
	var out MessagesSnapshot
	err := c.doJSON(ctx, http.MethodGet, "/v1/conversations/"+url.PathEscape(conversationID)+"/messages", nil, &out)
	return &out, err
}

func (c *Client) DeleteConversation(ctx context.Context, conversationID string) error {
	return c.doJSON(ctx, http.MethodDelete, "/v1/conversations/"+url.PathEscape(conversationID), nil, nil)
}

type FeedbackRequest struct {
	RunID   string  `json:"run_id"`
	Score   float64 `json:"score"`
	Comment string  `json:"comment,omitempty"`
}

func (c *Client) PostFeedback(ctx context.Context, feedback FeedbackRequest) error {
	return c.doJSON(ctx, http.MethodPost, "/v1/feedback", feedback, nil)
}

// Stream starts an agent run and returns the live event stream.
func (c *Client) Stream(ctx context.Context, input RunAgentInput) (*Stream, error) {
	req, err := c.newRequest(ctx, http.MethodPost, "/v1/agent", input)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := c.stream.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling Rico: %w", err)
	}
	if err := checkStatus(resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	return &Stream{body: resp.Body, reader: sse.NewReader(resp.Body)}, nil
}

// Stream yields decoded AG-UI events. Keepalive comments are skipped.
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
			return nil, fmt.Errorf("decoding Rico event: %w", err)
		}
		event.Raw = json.RawMessage(frame.Data)
		return &event, nil
	}
}

func (s *Stream) Close() error {
	return s.body.Close()
}
