package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/cli"
)

// ricoServer serves a canned agent run: one text reply, one tool call that
// interrupts, then success after resume.
func ricoServer(t *testing.T) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var inputs []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agent" {
			t.Errorf("unexpected path %s", r.URL.Path)
			return
		}
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Errorf("decode input: %v", err)
		}
		inputs = append(inputs, input)
		w.Header().Set("Content-Type", "text/event-stream")
		if _, isResume := input["resume"]; isResume {
			io.WriteString(w, "data: {\"type\":\"TEXT_MESSAGE_CONTENT\",\"messageId\":\"m2\",\"delta\":\"Deleted it.\"}\n\n")
			io.WriteString(w, "data: {\"type\":\"RUN_FINISHED\",\"threadId\":\"conv1\",\"runId\":\"r2\",\"outcome\":{\"type\":\"success\"}}\n\n")
			return
		}
		io.WriteString(w, "data: {\"type\":\"TEXT_MESSAGE_CONTENT\",\"messageId\":\"m1\",\"delta\":\"Sure — removing the offering.\"}\n\n")
		io.WriteString(w, "data: {\"type\":\"TOOL_CALL_START\",\"toolCallId\":\"tc1\",\"toolCallName\":\"delete_offering\"}\n\n")
		io.WriteString(w, "data: {\"type\":\"RUN_FINISHED\",\"threadId\":\"conv1\",\"runId\":\"r1\",\"outcome\":{\"type\":\"interrupt\",\"interrupts\":[{\"id\":\"int1\",\"reason\":\"tool_approval\",\"toolCallId\":\"tc1\",\"message\":\"Delete offering ofrng_test?\",\"metadata\":{\"is_destructive\":true}}]}}\n\n")
	}))
	t.Cleanup(server.Close)
	return server, &inputs
}

func TestRicoChat_JSONApprovesDestructiveToolWithYes(t *testing.T) {
	server, inputs := ricoServer(t)
	t.Setenv("RC_RICO_BASE_URL", server.URL)

	stdout, stderr, err := runCmd(t,
		"rico", "chat", "delete the test offering",
		"--conversation", "conv1",
		"--approve-tools", "--yes", "--no-input", "--json",
		"--api-key", "sk_test",
	)
	if err != nil {
		t.Fatalf("err = %v, stderr %s", err, stderr)
	}
	var envelope struct {
		Data struct {
			ConversationID string `json:"conversation_id"`
			Reply          string `json:"reply"`
			Status         string `json:"status"`
			ToolCalls      []struct {
				Name string `json:"name"`
			} `json:"tool_calls"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, stdout)
	}
	if envelope.Data.ConversationID != "conv1" || envelope.Data.Status != "success" {
		t.Fatalf("data = %+v", envelope.Data)
	}
	if envelope.Data.Reply != "Sure — removing the offering.Deleted it." {
		t.Fatalf("reply = %q", envelope.Data.Reply)
	}
	if len(envelope.Data.ToolCalls) != 1 || envelope.Data.ToolCalls[0].Name != "delete_offering" {
		t.Fatalf("tool calls = %+v", envelope.Data.ToolCalls)
	}

	if len(*inputs) != 2 {
		t.Fatalf("expected chat + resume requests, got %d", len(*inputs))
	}
	resume := (*inputs)[1]["resume"].([]any)[0].(map[string]any)
	if resume["interruptId"] != "tc1" || resume["status"] != "resolved" {
		t.Fatalf("resume = %v", resume)
	}
	if approved := resume["payload"].(map[string]any)["approved"]; approved != true {
		t.Fatalf("approved = %v", approved)
	}
}

func TestRicoChat_JSONRejectsDestructiveToolWithoutYes(t *testing.T) {
	server, inputs := ricoServer(t)
	t.Setenv("RC_RICO_BASE_URL", server.URL)

	stdout, _, err := runCmd(t,
		"rico", "chat", "delete the test offering",
		"--conversation", "conv1",
		"--approve-tools", "--no-input", "--json",
		"--api-key", "sk_test",
	)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			Rejected []struct {
				ID string `json:"id"`
			} `json:"rejected_tool_calls"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, stdout)
	}
	if len(envelope.Data.Rejected) != 1 || envelope.Data.Rejected[0].ID != "tc1" {
		t.Fatalf("rejected = %+v", envelope.Data.Rejected)
	}
	resume := (*inputs)[1]["resume"].([]any)[0].(map[string]any)
	if approved := resume["payload"].(map[string]any)["approved"]; approved != false {
		t.Fatalf("approved = %v", approved)
	}
}

func TestRicoChat_ContinueResumesLastConversation(t *testing.T) {
	server, inputs := ricoServer(t)
	t.Setenv("RC_RICO_BASE_URL", server.URL)

	configDir := t.TempDir()
	// First turn establishes the conversation and records it.
	_, _, err := runCmdInConfigDir(t, configDir,
		"rico", "chat", "delete the test offering",
		"--conversation", "conv1",
		"--approve-tools", "--yes", "--no-input", "--json", "--api-key", "sk_test",
	)
	if err != nil {
		t.Fatal(err)
	}
	// --continue picks the same conversation back up without an explicit id.
	_, _, err = runCmdInConfigDir(t, configDir,
		"rico", "chat", "and the sandbox one too", "--continue",
		"--approve-tools", "--yes", "--no-input", "--json", "--api-key", "sk_test",
	)
	if err != nil {
		t.Fatal(err)
	}
	last := (*inputs)[len(*inputs)-1]
	if last["threadId"] != "conv1" {
		t.Fatalf("continued threadId = %v", last["threadId"])
	}
}

func TestRicoChat_ContinueWithoutHistoryFails(t *testing.T) {
	_, _, err := runCmd(t, "rico", "chat", "hi", "--continue", "--no-input", "--api-key", "sk_test")
	if err == nil || !strings.Contains(err.Error(), "no previous conversation") {
		t.Fatalf("err = %v", err)
	}
}

func TestRicoChat_RequiresPromptNonInteractive(t *testing.T) {
	_, _, err := runCmd(t, "rico", "chat", "--no-input", "--api-key", "sk_test")
	if err == nil || !strings.Contains(err.Error(), "message is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestRicoConversations_ListJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/conversations" {
			t.Errorf("path = %s", r.URL.Path)
		}
		io.WriteString(w, `[{"id":"conv1","summary":"Refunds","created_at":"2026-07-01T00:00:00Z","updated_at":"2026-07-02T00:00:00Z"}]`)
	}))
	defer server.Close()
	t.Setenv("RC_RICO_BASE_URL", server.URL)

	stdout, _, err := runCmd(t, "rico", "conversations", "list", "--json", "--no-input", "--api-key", "sk_test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"conv1"`) || !strings.Contains(stdout, "Refunds") {
		t.Fatalf("stdout = %s", stdout)
	}
}

// astraServers stubs both the v2 API (draft creation) and the Astra editor.
func astraTestServers(t *testing.T) (apiURL, astraURL string, editorInputs *[]map[string]any) {
	t.Helper()
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/paywalls"):
			io.WriteString(w, `{"id":"pw_new","offering_id":"ofrng_default","created_at":1720000000000,"published_at":null}`)
		default:
			t.Errorf("unexpected API request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(apiServer.Close)

	var inputs []map[string]any
	astraServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/editor/v1/stream" {
			t.Errorf("astra path = %s", r.URL.Path)
			return
		}
		var input map[string]any
		json.NewDecoder(r.Body).Decode(&input)
		inputs = append(inputs, input)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"run.started\",\"session_id\":\"sess1\"}\n\n")
		io.WriteString(w, "data: {\"type\":\"turn.snapshot\",\"session_id\":\"sess1\",\"turn_index\":0,\"paywall\":{\"default_locale\":\"en_US\",\"offering_id\":\"ofrng_default\",\"components_config\":{\"stack\":true},\"components_localizations\":{\"en_US\":{}}},\"activity\":[{\"id\":\"a1\",\"type\":\"tool\",\"tool_name\":\"edit_components\",\"status\":\"success\",\"display\":{\"text\":\"Built hero section\"}}],\"__unstable_session_items\":[{\"k\":1}]}\n\n")
		io.WriteString(w, "data: {\"type\":\"run.completed\",\"session_id\":\"sess1\",\"trace_id\":\"tr1\",\"paywall\":{\"default_locale\":\"en_US\",\"offering_id\":\"ofrng_default\",\"components_config\":{\"stack\":true},\"components_localizations\":{\"en_US\":{}}},\"activity\":[{\"id\":\"a1\",\"type\":\"tool\",\"tool_name\":\"edit_components\",\"status\":\"success\",\"display\":{\"text\":\"Built hero section\"}},{\"id\":\"a2\",\"type\":\"assistant_message\",\"content\":\"Done — calm annual-first layout.\"}],\"__unstable_session_items\":[{\"k\":2}]}\n\n")
	}))
	t.Cleanup(astraServer.Close)
	return apiServer.URL, astraServer.URL, &inputs
}

// runAgentCmd is runCmd without the env reset, so RC_BASE_URL /
// RC_ASTRA_BASE_URL stubs set by the test survive.
func runAgentCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	var out, errb bytes.Buffer
	root := cli.NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), errb.String(), err
}

func TestPaywallsGenerate_CreatesDraftStreamsAndSavesSession(t *testing.T) {
	apiURL, astraURL, editorInputs := astraTestServers(t)
	t.Setenv("RC_BASE_URL", apiURL)
	t.Setenv("RC_ASTRA_BASE_URL", astraURL)

	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.json")
	stdout, _, err := runAgentCmd(t,
		"paywalls", "generate", "ofrng_default",
		"--prompt", "A calm annual-first paywall",
		"--session", sessionPath,
		"--project-id", "proj1",
		"--json", "--no-input", "--api-key", "sk_test",
	)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			PaywallID   string `json:"paywall_id"`
			SessionID   string `json:"session_id"`
			TraceID     string `json:"trace_id"`
			SessionFile string `json:"session_file"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, stdout)
	}
	if envelope.Data.PaywallID != "pw_new" || envelope.Data.SessionID != "sess1" || envelope.Data.TraceID != "tr1" {
		t.Fatalf("data = %+v", envelope.Data)
	}

	// The editor request carried the project, paywall, prompt, and state.
	input := (*editorInputs)[0]
	if input["project_id"] != "proj1" || input["paywall_id"] != "pw_new" || input["message"] != "A calm annual-first paywall" {
		t.Fatalf("editor input = %v", input)
	}

	// Session file round-trips the completed paywall + opaque blobs.
	payload, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	var session map[string]any
	if err := json.Unmarshal(payload, &session); err != nil {
		t.Fatal(err)
	}
	if session["session_id"] != "sess1" || session["paywall_id"] != "pw_new" {
		t.Fatalf("session = %v", session)
	}
	items := session["__unstable_session_items"].([]any)[0].(map[string]any)
	if items["k"] != 2.0 {
		t.Fatalf("session items = %v", session["__unstable_session_items"])
	}

	// Editing continues the same session with its saved state.
	stdout, _, err = runAgentCmd(t,
		"paywalls", "edit",
		"--session", sessionPath,
		"--prompt", "Make annual the visual default",
		"--json", "--no-input", "--api-key", "sk_test",
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"sess1"`) {
		t.Fatalf("edit stdout = %s", stdout)
	}
	editInput := (*editorInputs)[1]
	if editInput["session_id"] != "sess1" || editInput["message"] != "Make annual the visual default" {
		t.Fatalf("edit input = %v", editInput)
	}
	paywall := editInput["paywall"].(map[string]any)
	config := paywall["components_config"].(map[string]any)
	if config["stack"] != true {
		t.Fatalf("components_config not round-tripped: %v", paywall)
	}
}

func TestPaywallsEdit_RequiresSessionFile(t *testing.T) {
	_, _, err := runCmd(t, "paywalls", "edit", "--prompt", "x", "--no-input", "--api-key", "sk_test")
	if err == nil || !strings.Contains(err.Error(), "--session is required") {
		t.Fatalf("err = %v", err)
	}
}
