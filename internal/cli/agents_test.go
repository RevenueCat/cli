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
			io.WriteString(w, "data: {\"type\":\"TEXT_MESSAGE_START\",\"messageId\":\"m2\",\"role\":\"assistant\"}\n\n")
			io.WriteString(w, "data: {\"type\":\"TEXT_MESSAGE_CONTENT\",\"messageId\":\"m2\",\"delta\":\"Deleted it.\"}\n\n")
			io.WriteString(w, "data: {\"type\":\"RUN_FINISHED\",\"threadId\":\"conv1\",\"runId\":\"r2\",\"outcome\":{\"type\":\"success\"}}\n\n")
			return
		}
		io.WriteString(w, "data: {\"type\":\"TEXT_MESSAGE_START\",\"messageId\":\"m1\",\"role\":\"assistant\"}\n\n")
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
	if envelope.Data.Reply != "Sure — removing the offering.\n\nDeleted it." {
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

func TestRicoChat_RemembersLastConversationForResume(t *testing.T) {
	server, _ := ricoServer(t)
	t.Setenv("RC_RICO_BASE_URL", server.URL)

	configDir := t.TempDir()
	_, _, err := runCmdInConfigDir(t, configDir,
		"rico", "chat", "delete the test offering",
		"--conversation", "conv1",
		"--approve-tools", "--yes", "--no-input", "--json", "--api-key", "sk_test",
	)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(configDir, "default.rico.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		LastConversationID string `json:"last_conversation_id"`
	}
	if err := json.Unmarshal(payload, &state); err != nil {
		t.Fatal(err)
	}
	if state.LastConversationID != "conv1" {
		t.Fatalf("state = %+v", state)
	}
}

func TestRicoChat_ResumeRequiresInteractivePicker(t *testing.T) {
	_, _, err := runCmd(t, "rico", "chat", "hi", "--resume", "--no-input", "--api-key", "sk_test")
	if err == nil || !strings.Contains(err.Error(), "conversation ID is required") {
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

// astraTestServers stubs both the v2 API (draft creation) and the Astra
// editor. offeringID == "" makes the stubs serve a standalone paywall
// (offering_id null everywhere).
func astraTestServers(t *testing.T, offeringID string) (apiURL, astraURL string, editorInputs, createInputs *[]map[string]any) {
	t.Helper()
	offeringJSON := "null"
	if offeringID != "" {
		offeringJSON = `"` + offeringID + `"`
	}
	var created []map[string]any
	var patched []map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/paywalls"):
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			created = append(created, body)
			io.WriteString(w, `{"id":"pw_new","offering_id":`+offeringJSON+`,"created_at":1720000000000,"published_at":null}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/paywalls/pw_new"):
			io.WriteString(w, `{"id":"pw_new","offering_id":`+offeringJSON+`,"created_at":1720000000000,"published_at":null,"components":{"published":null,"draft":{"revision":3,"components_config":{},"components_localizations":{},"default_locale":"en_US","automatically_scale_font_size":true}}}`)
		case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/paywalls/pw_new"):
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			patched = append(patched, body)
			io.WriteString(w, `{"id":"pw_new","offering_id":`+offeringJSON+`,"created_at":1720000000000,"published_at":null,"components":{"published":null,"draft":{"revision":4,"components_config":{},"components_localizations":{},"default_locale":"en_US","automatically_scale_font_size":true}}}`)
		default:
			t.Errorf("unexpected API request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(func() {
		if len(patched) == 0 {
			t.Error("design was never PATCHed onto the RevenueCat draft")
			return
		}
		last := patched[len(patched)-1]
		if last["revision"] != 3.0 {
			t.Errorf("PATCH revision = %v", last["revision"])
		}
		config, _ := last["components_config"].(map[string]any)
		if config["stack"] != true {
			t.Errorf("PATCH components_config = %v", last["components_config"])
		}
	})
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
		io.WriteString(w, "data: {\"type\":\"turn.snapshot\",\"session_id\":\"sess1\",\"turn_index\":0,\"paywall\":{\"default_locale\":\"en_US\",\"offering_id\":"+offeringJSON+",\"components_config\":{\"stack\":true},\"components_localizations\":{\"en_US\":{}}},\"activity\":[{\"id\":\"a1\",\"type\":\"tool\",\"tool_name\":\"edit_components\",\"status\":\"success\",\"display\":{\"text\":\"Built hero section\"}}],\"__unstable_session_items\":[{\"k\":1}]}\n\n")
		io.WriteString(w, "data: {\"type\":\"run.completed\",\"session_id\":\"sess1\",\"trace_id\":\"tr1\",\"paywall\":{\"default_locale\":\"en_US\",\"offering_id\":"+offeringJSON+",\"components_config\":{\"stack\":true},\"components_localizations\":{\"en_US\":{}}},\"activity\":[{\"id\":\"a1\",\"type\":\"tool\",\"tool_name\":\"edit_components\",\"status\":\"success\",\"display\":{\"text\":\"Built hero section\"}},{\"id\":\"a2\",\"type\":\"assistant_message\",\"content\":\"Done — calm annual-first layout.\"}],\"__unstable_session_items\":[{\"k\":2}]}\n\n")
	}))
	t.Cleanup(astraServer.Close)
	return apiServer.URL, astraServer.URL, &inputs, &created
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
	apiURL, astraURL, editorInputs, createInputs := astraTestServers(t, "ofrng_default")
	t.Setenv("RC_BASE_URL", apiURL)
	t.Setenv("RC_ASTRA_BASE_URL", astraURL)

	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.json")
	stdout, _, err := runAgentCmd(t,
		"paywalls", "generate", "--offering-id", "ofrng_default", "--name", "Annual push",
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
	create := (*createInputs)[0]
	if create["offering_id"] != "ofrng_default" || create["name"] != "Annual push" {
		t.Fatalf("create body = %v", create)
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

func TestPaywallsGenerate_Standalone(t *testing.T) {
	apiURL, astraURL, editorInputs, createInputs := astraTestServers(t, "")
	t.Setenv("RC_BASE_URL", apiURL)
	t.Setenv("RC_ASTRA_BASE_URL", astraURL)
	t.Setenv("RC_OFFERING_ID", "")

	sessionPath := filepath.Join(t.TempDir(), "session.json")
	_, _, err := runAgentCmd(t,
		"paywalls", "generate", "--name", "Summer sale",
		"--prompt", "A calm annual-first paywall",
		"--session", sessionPath,
		"--project-id", "proj1",
		"--json", "--no-input", "--api-key", "sk_test",
	)
	if err != nil {
		t.Fatal(err)
	}

	// The draft was created from components, not attached to an offering.
	create := (*createInputs)[0]
	if _, ok := create["offering_id"]; ok {
		t.Fatalf("create body should not carry offering_id: %v", create)
	}
	if create["name"] != "Summer sale" || create["default_locale"] != "en_US" {
		t.Fatalf("create body = %v", create)
	}
	if _, ok := create["components_config"]; !ok {
		t.Fatalf("create body missing components_config: %v", create)
	}
	if _, ok := create["components_localizations"]; !ok {
		t.Fatalf("create body missing components_localizations: %v", create)
	}

	// The editor request carried a null offering.
	paywall := (*editorInputs)[0]["paywall"].(map[string]any)
	if v, ok := paywall["offering_id"]; !ok || v != nil {
		t.Fatalf("editor paywall.offering_id = %v (present %v)", v, ok)
	}

	// The session file round-trips the null offering.
	payload, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	var session map[string]any
	if err := json.Unmarshal(payload, &session); err != nil {
		t.Fatal(err)
	}
	if v, ok := session["paywall"].(map[string]any)["offering_id"]; !ok || v != nil {
		t.Fatalf("session paywall.offering_id = %v (present %v)", v, ok)
	}
}

// An offering can only have one paywall; the server's bare 409 must come back
// with the ways out, not as a raw API error.
func TestPaywallsGenerate_OfferingAlreadyHasPaywall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		io.WriteString(w, `{"object":"error","type":"resource_already_exists","message":"Paywall already exists for this offering"}`)
	}))
	t.Cleanup(server.Close)
	t.Setenv("RC_BASE_URL", server.URL)

	_, _, err := runAgentCmd(t,
		"paywalls", "generate", "--offering-id", "ofrng_default",
		"--prompt", "A calm annual-first paywall",
		"--project-id", "proj1", "--no-input", "--api-key", "sk_test",
	)
	if err == nil || !strings.Contains(err.Error(), "ofrng_default already has a paywall") || !strings.Contains(err.Error(), "omit --offering-id") {
		t.Fatalf("err = %v", err)
	}
}

func TestPaywallsEdit_RequiresSessionOrID(t *testing.T) {
	_, _, err := runCmd(t, "paywalls", "edit", "--prompt", "x", "--no-input", "--api-key", "sk_test")
	if err == nil || !strings.Contains(err.Error(), "pass a paywall ID or --session") {
		t.Fatalf("err = %v", err)
	}
}
