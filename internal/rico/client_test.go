package rico

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStreamDecodesEvents(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agent" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok_123" {
			t.Errorf("Authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"RUN_STARTED\",\"threadId\":\"conv1\",\"runId\":\"run1\"}\n\n")
		io.WriteString(w, ": keepalive\n\n")
		io.WriteString(w, "data: {\"type\":\"TEXT_MESSAGE_CONTENT\",\"messageId\":\"m1\",\"delta\":\"Hello\"}\n\n")
		io.WriteString(w, "data: {\"type\":\"RUN_FINISHED\",\"threadId\":\"conv1\",\"runId\":\"run1\",\"outcome\":{\"type\":\"interrupt\",\"interrupts\":[{\"id\":\"int1\",\"reason\":\"tool_approval\",\"toolCallId\":\"tc1\",\"metadata\":{\"is_destructive\":true}}]}}\n\n")
	}))
	defer server.Close()

	client := NewClient(Options{BaseURL: server.URL, Token: "tok_123"})
	stream, err := client.Stream(context.Background(), ChatInput("conv1", "run1", "hi", DashboardContext{ProjectID: "proj1", Pathname: "/cli"}, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	first, err := stream.Next()
	if err != nil || first.Type != EventRunStarted || first.RunID != "run1" {
		t.Fatalf("first event = %+v, err %v", first, err)
	}
	second, err := stream.Next()
	if err != nil || second.Delta != "Hello" {
		t.Fatalf("second event = %+v, err %v", second, err)
	}
	third, err := stream.Next()
	if err != nil || third.Type != EventRunFinished {
		t.Fatalf("third event = %+v, err %v", third, err)
	}
	if third.Outcome == nil || third.Outcome.Type != "interrupt" {
		t.Fatalf("outcome = %+v", third.Outcome)
	}
	interrupt := third.Outcome.Interrupts[0]
	if !interrupt.IsDestructive() || interrupt.ResumeID() != "tc1" {
		t.Fatalf("interrupt = %+v", interrupt)
	}
	if _, err := stream.Next(); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}

	// Request body matches the AG-UI shape the first-party clients send.
	if gotBody["threadId"] != "conv1" || gotBody["runId"] != "run1" {
		t.Errorf("body ids = %v %v", gotBody["threadId"], gotBody["runId"])
	}
	if _, hasState := gotBody["state"]; !hasState || gotBody["state"] != nil {
		t.Errorf("state should be JSON null, got %v", gotBody["state"])
	}
	forwarded := gotBody["forwardedProps"].(map[string]any)["dashboard_context"].(map[string]any)
	if forwarded["project_id"] != "proj1" || forwarded["pathname"] != "/cli" {
		t.Errorf("dashboard_context = %v", forwarded)
	}
	messages := gotBody["messages"].([]any)
	message := messages[0].(map[string]any)
	if message["role"] != "user" || message["content"] != "hi" {
		t.Errorf("message = %v", message)
	}
}

func TestStreamRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "17")
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"detail":"slow down"}`)
	}))
	defer server.Close()

	client := NewClient(Options{BaseURL: server.URL, Token: "tok"})
	_, err := client.Stream(context.Background(), ChatInput("c", "r", "hi", DashboardContext{Pathname: "/"}, nil))
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 429 || apiErr.RetryAfter != 17*time.Second || apiErr.Message != "slow down" {
		t.Fatalf("err = %v", err)
	}
}

func TestListConversationsAndMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/conversations":
			if r.URL.Query().Get("project_id") != "proj1" {
				t.Errorf("project_id = %q", r.URL.Query().Get("project_id"))
			}
			io.WriteString(w, `[{"id":"conv1","summary":"Refund flow","created_at":"2026-07-01T00:00:00Z","updated_at":"2026-07-02T00:00:00Z"}]`)
		case r.URL.Path == "/v1/conversations/conv1/messages":
			io.WriteString(w, `{"type":"MESSAGES_SNAPSHOT","messages":[{"id":"m1","role":"assistant","content":[{"type":"text","text":"part1"},{"type":"text","text":"part2"}]}],"pendingInterrupts":[{"id":"int1","reason":"tool_approval"}]}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(Options{BaseURL: server.URL, Token: "tok"})
	conversations, err := client.ListConversations(context.Background(), "proj1")
	if err != nil || len(conversations) != 1 || conversations[0].Summary != "Refund flow" {
		t.Fatalf("conversations = %+v, err %v", conversations, err)
	}
	snapshot, err := client.GetMessages(context.Background(), "conv1")
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot.Messages[0].Text(); got != "part1\npart2" {
		t.Fatalf("text = %q", got)
	}
	if len(snapshot.PendingInterrupts) != 1 {
		t.Fatalf("interrupts = %+v", snapshot.PendingInterrupts)
	}
}

func TestPostFeedback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["run_id"] != "run1" || body["score"] != 1.0 || body["comment"] != "great" {
			t.Errorf("body = %v", body)
		}
	}))
	defer server.Close()
	client := NewClient(Options{BaseURL: server.URL, Token: "tok"})
	if err := client.PostFeedback(context.Background(), FeedbackRequest{RunID: "run1", Score: 1, Comment: "great"}); err != nil {
		t.Fatal(err)
	}
}

func TestNewConversationID(t *testing.T) {
	id := NewConversationID()
	if len(id) != 16 {
		t.Fatalf("len = %d", len(id))
	}
	for _, r := range id {
		if !strings.ContainsRune(conversationAlphabet, r) {
			t.Fatalf("unexpected rune %q in %q", r, id)
		}
	}
	if !strings.HasPrefix(NewRunID(), "rico_cli_") {
		t.Fatal("run id prefix")
	}
}
