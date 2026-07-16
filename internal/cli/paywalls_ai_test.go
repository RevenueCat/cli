package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type mcpToolCall struct {
	Params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"params"`
}

func TestPaywallsGenerateStartsAsyncAstraTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk_test" {
			t.Fatalf("authorization = %q", got)
		}
		var call mcpToolCall
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			t.Fatal(err)
		}
		if call.Params.Name != "create-paywall-ai" {
			t.Fatalf("tool = %q", call.Params.Name)
		}
		if call.Params.Arguments["project_id"] != "proj" || call.Params.Arguments["offering_id"] != "ofrng_default" {
			t.Fatalf("arguments = %+v", call.Params.Arguments)
		}
		if call.Params.Arguments["prompt"] != "A calm annual-first paywall" {
			t.Fatalf("prompt = %v", call.Params.Arguments["prompt"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Paywall AI Editor creation started.\nTask: task_123\nStatus: queued\nMessage: Paywall task queued.\nPoll: call get-paywall-ai-task with task_id \"task_123\" in about 30 seconds."}]}}`)
	}))
	t.Cleanup(server.Close)

	out, _, err := runProjectSetupCommand(t, server.URL,
		"paywalls", "generate", "ofrng_default",
		"--prompt", "A calm annual-first paywall",
		"--mcp-url", server.URL,
		"--async", "--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"task_id": "task_123"`) || !strings.Contains(out, `"status": "queued"`) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestPaywallsEditWaitsForAstraDraft(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var call mcpToolCall
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch calls.Add(1) {
		case 1:
			if call.Params.Name != "edit-paywall-ai" || call.Params.Arguments["paywall_id"] != "pw_test" {
				t.Fatalf("unexpected call: %+v", call)
			}
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Paywall AI Editor edit started.\nTask: task_edit\nStatus: queued\nMessage: Paywall task queued.\nPoll: call get-paywall-ai-task with task_id \"task_edit\" in about 1 seconds."}]}}`)
		case 2:
			if call.Params.Name != "get-paywall-ai-task" || call.Params.Arguments["task_id"] != "task_edit" {
				t.Fatalf("unexpected call: %+v", call)
			}
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Paywall updated and saved as an unpublished draft.\nProject: proj\nOffering: ofrng_default\nPaywall: pw_test\nEditor: https://app.revenuecat.com/projects/test/paywalls/pw_test/builder\nAssistant: Updated annual emphasis."},{"type":"image","data":"aW1hZ2U=","mimeType":"image/png"}]}}`)
		default:
			t.Fatalf("unexpected extra call")
		}
	}))
	t.Cleanup(server.Close)

	out, _, err := runProjectSetupCommand(t, server.URL,
		"paywalls", "edit", "pw_test",
		"--prompt", "Make annual the visual default",
		"--mcp-url", server.URL,
		"--timeout", "1s", "--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"status": "succeeded"`) || !strings.Contains(out, `"paywall_id": "pw_test"`) || !strings.Contains(out, `"screenshot_count": 1`) {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestPaywallsGenerateRequiresPromptWithoutInput(t *testing.T) {
	_, _, err := runProjectSetupCommand(t, "http://unused.test",
		"paywalls", "generate", "ofrng_default", "--async", "--json", "--no-input")
	if err == nil || !strings.Contains(err.Error(), "prompt is required") {
		t.Fatalf("error = %v", err)
	}
}
