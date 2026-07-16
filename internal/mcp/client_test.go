package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revenuecat/cli/internal/mcp"
)

func TestCallToolSendsAuthenticatedJSONRPC(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer oauth-token" {
			t.Fatalf("authorization = %q", got)
		}
		var request struct {
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Method != "tools/call" || request.Params.Name != "edit-paywall-ai" {
			t.Fatalf("unexpected request: %+v", request)
		}
		if request.Params.Arguments["paywall_id"] != "pw_test" {
			t.Fatalf("arguments = %+v", request.Params.Arguments)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Task: task_123"}]}}`))
	}))
	t.Cleanup(server.Close)

	client := mcp.NewClient(mcp.Options{URL: server.URL, Token: "oauth-token"})
	result, err := client.CallTool(context.Background(), "edit-paywall-ai", map[string]any{"paywall_id": "pw_test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text() != "Task: task_123" {
		t.Fatalf("text = %q", result.Text())
	}
}

func TestCallToolDecodesEventStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"Status: running\"}]}}\n\n"))
	}))
	t.Cleanup(server.Close)

	client := mcp.NewClient(mcp.Options{URL: server.URL, Token: "token"})
	result, err := client.CallTool(context.Background(), "get-paywall-ai-task", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Text() != "Status: running" {
		t.Fatalf("text = %q", result.Text())
	}
}

func TestCallToolReturnsToolErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"isError":true,"content":[{"type":"text","text":"Paywall revision is stale"}]}}`))
	}))
	t.Cleanup(server.Close)

	client := mcp.NewClient(mcp.Options{URL: server.URL, Token: "token"})
	_, err := client.CallTool(context.Background(), "edit-paywall-ai", nil)
	if err == nil || err.Error() != "Paywall AI Editor: Paywall revision is stale" {
		t.Fatalf("error = %v", err)
	}
}
