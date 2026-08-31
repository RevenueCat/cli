package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		"rico", "delete the test offering",
		"--conversation", "conv1",
		"--approve-tools", "--yes", "--no-input", "--json",
		"--api-key", "atk_test",
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
		"rico", "delete the test offering",
		"--conversation", "conv1",
		"--approve-tools", "--no-input", "--json",
		"--api-key", "atk_test",
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
		"rico", "delete the test offering",
		"--conversation", "conv1",
		"--approve-tools", "--yes", "--no-input", "--json", "--api-key", "atk_test",
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
	_, _, err := runCmd(t, "rico", "hi", "--resume", "--no-input", "--api-key", "atk_test")
	if err == nil || !strings.Contains(err.Error(), "conversation ID is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestRicoChat_RequiresPromptNonInteractive(t *testing.T) {
	_, _, err := runCmd(t, "rico", "--no-input", "--api-key", "atk_test")
	if err == nil || !strings.Contains(err.Error(), "message is required") {
		t.Fatalf("err = %v", err)
	}
}

// --json is non-interactive: no message must error, not open the chat UI.
func TestRico_JSONWithoutMessageRequiresMessage(t *testing.T) {
	_, _, err := runCmd(t, "rico", "--json", "--api-key", "atk_test")
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

	stdout, _, err := runCmd(t, "rico", "conversations", "list", "--json", "--no-input", "--api-key", "atk_test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, `"conv1"`) || !strings.Contains(stdout, "Refunds") {
		t.Fatalf("stdout = %s", stdout)
	}
}

// paywallResponseJSON is the v2 API paywall body for pw_new at a given
// offering and draft revision.
func paywallResponseJSON(offeringJSON string, revision int) string {
	return fmt.Sprintf(`{"id":"pw_new","offering_id":%s,"created_at":1720000000000,"published_at":null,"components":{"published":null,"draft":{"revision":%d,"components_config":{},"components_localizations":{},"default_locale":"en_US","automatically_scale_font_size":true}}}`, offeringJSON, revision)
}

// stubEditorServer serves one minimal completed design turn and counts the
// requests it received.
func stubEditorServer(t *testing.T) (*httptest.Server, *int) {
	t.Helper()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"run.started\",\"session_id\":\"sess1\"}\n\n")
		io.WriteString(w, "data: {\"type\":\"run.completed\",\"session_id\":\"sess1\",\"trace_id\":\"tr1\",\"paywall\":{\"default_locale\":\"en_US\",\"offering_id\":null,\"components_config\":{\"stack\":true},\"components_localizations\":{\"en_US\":{}}},\"activity\":[],\"__unstable_session_items\":[{\"k\":3}]}\n\n")
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

// paywallAITestServers stubs both the v2 API (draft creation) and the Paywall AI
// editor. offeringID == "" makes the v2 API stubs serve a standalone paywall.
// The editor stream echoes offering_id as null, as the live service does
// after a template load.
func paywallAITestServers(t *testing.T, offeringID string) (apiURL, paywallAIURL string, editorInputs, createInputs *[]map[string]any) {
	t.Helper()
	offeringJSON := "null"
	if offeringID != "" {
		offeringJSON = `"` + offeringID + `"`
	}
	var created []map[string]any
	var patched []map[string]any
	// The draft revision is stateful like the backend's: GET serves the current
	// one, a PATCH must carry it (the conflict guard) and bumps it.
	revision := 3
	paywallJSON := func() string { return paywallResponseJSON(offeringJSON, revision) }
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/paywalls"):
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			created = append(created, body)
			io.WriteString(w, `{"id":"pw_new","offering_id":`+offeringJSON+`,"created_at":1720000000000,"published_at":null}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/paywalls/pw_new"):
			io.WriteString(w, paywallJSON())
		case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/paywalls/pw_new"):
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["revision"] != float64(revision) {
				t.Errorf("PATCH revision = %v, server draft is at %d", body["revision"], revision)
			}
			patched = append(patched, body)
			revision++
			io.WriteString(w, paywallJSON())
		default:
			t.Errorf("unexpected API request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(func() {
		if len(patched) == 0 {
			t.Error("design was never PATCHed onto the RevenueCat draft")
			return
		}
		config, _ := patched[len(patched)-1]["components_config"].(map[string]any)
		if config["stack"] != true {
			t.Errorf("PATCH components_config = %v", patched[len(patched)-1]["components_config"])
		}
	})
	t.Cleanup(apiServer.Close)

	var inputs []map[string]any
	paywallAIServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/editor/v1/stream" {
			t.Errorf("paywall AI path = %s", r.URL.Path)
			return
		}
		var input map[string]any
		json.NewDecoder(r.Body).Decode(&input)
		inputs = append(inputs, input)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"run.started\",\"session_id\":\"sess1\"}\n\n")
		io.WriteString(w, "data: {\"type\":\"turn.snapshot\",\"session_id\":\"sess1\",\"turn_index\":0,\"paywall\":{\"default_locale\":\"en_US\",\"offering_id\":null,\"components_config\":{\"stack\":true},\"components_localizations\":{\"en_US\":{}}},\"activity\":[{\"id\":\"a1\",\"type\":\"tool\",\"tool_name\":\"edit_components\",\"status\":\"success\",\"display\":{\"text\":\"Built hero section\"}}],\"__unstable_session_items\":[{\"k\":1}]}\n\n")
		io.WriteString(w, "data: {\"type\":\"run.completed\",\"session_id\":\"sess1\",\"trace_id\":\"tr1\",\"paywall\":{\"default_locale\":\"en_US\",\"offering_id\":null,\"components_config\":{\"stack\":true},\"components_localizations\":{\"en_US\":{}}},\"activity\":[{\"id\":\"a1\",\"type\":\"tool\",\"tool_name\":\"edit_components\",\"status\":\"success\",\"display\":{\"text\":\"Built hero section\"}},{\"id\":\"a2\",\"type\":\"assistant_message\",\"content\":\"Done — calm annual-first layout.\"}],\"__unstable_session_items\":[{\"k\":2}],\"result_screenshots\":[{\"color_scheme\":\"light\",\"mime_type\":\"image/png\",\"data_base64\":\"UE5H\"}]}\n\n")
	}))
	t.Cleanup(paywallAIServer.Close)
	return apiServer.URL, paywallAIServer.URL, &inputs, &created
}

// runAgentCmd is runCmd without the env reset, so RC_BASE_URL /
// RC_PAYWALL_AI_BASE_URL stubs set by the test survive.
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
	apiURL, paywallAIURL, editorInputs, createInputs := paywallAITestServers(t, "ofrng_default")
	t.Setenv("RC_BASE_URL", apiURL)
	t.Setenv("RC_PAYWALL_AI_BASE_URL", paywallAIURL)

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
			PaywallID       string            `json:"paywall_id"`
			SessionID       string            `json:"session_id"`
			TraceID         string            `json:"trace_id"`
			SessionFile     string            `json:"session_file"`
			ScreenshotPaths map[string]string `json:"screenshot_paths"`
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

	// The run's preview screenshot landed next to the session file and its
	// path is in the envelope.
	shotPath := envelope.Data.ScreenshotPaths["light"]
	if shotPath == "" {
		t.Fatalf("screenshot_paths = %v", envelope.Data.ScreenshotPaths)
	}
	if shot, err := os.ReadFile(shotPath); err != nil || string(shot) != "PNG" {
		t.Fatalf("screenshot at %s = %q, err %v", shotPath, shot, err)
	}

	// The editor request carried the project, paywall, prompt, and state.
	input := (*editorInputs)[0]
	if input["project_id"] != "proj1" || input["paywall_id"] != "pw_new" || input["message"] != "A calm annual-first paywall" {
		t.Fatalf("editor input = %v", input)
	}
	if input["include_result_screenshots"] != true {
		t.Fatalf("include_result_screenshots = %v", input["include_result_screenshots"])
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
	// The editor echoes offering_id as null; the session keeps the attached offering.
	if v := session["paywall"].(map[string]any)["offering_id"]; v != "ofrng_default" {
		t.Fatalf("session paywall.offering_id = %v", v)
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
	if paywall["offering_id"] != "ofrng_default" {
		t.Fatalf("edit paywall.offering_id = %v", paywall["offering_id"])
	}
}

func TestPaywallsGenerate_Standalone(t *testing.T) {
	apiURL, paywallAIURL, editorInputs, createInputs := paywallAITestServers(t, "")
	t.Setenv("RC_BASE_URL", apiURL)
	t.Setenv("RC_PAYWALL_AI_BASE_URL", paywallAIURL)
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

// Offering attachment can change out-of-band (dashboard). The API stub
// reports the paywall attached even though generate ran standalone — the
// session must pick up the server truth from the PATCH response.
func TestPaywallsGenerate_RefreshesOfferingFromServer(t *testing.T) {
	apiURL, paywallAIURL, _, _ := paywallAITestServers(t, "ofrng_default")
	t.Setenv("RC_BASE_URL", apiURL)
	t.Setenv("RC_PAYWALL_AI_BASE_URL", paywallAIURL)
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

	payload, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	var session map[string]any
	if err := json.Unmarshal(payload, &session); err != nil {
		t.Fatal(err)
	}
	if v := session["paywall"].(map[string]any)["offering_id"]; v != "ofrng_default" {
		t.Fatalf("session paywall.offering_id = %v", v)
	}
}

// A dashboard edit that lands during generate's multi-minute design turn must
// surface as a conflict, not be clobbered: the PATCH carries the revision
// fetched once at create time — refetching at persist would sail past
// the backend's conflict guard.
func TestPaywallsGenerate_DraftChangedDuringRun(t *testing.T) {
	gets := 0
	var patched []map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/paywalls"):
			io.WriteString(w, `{"id":"pw_new","offering_id":null,"created_at":1720000000000,"published_at":null}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/paywalls/pw_new"):
			gets++
			revision := 3 // at create time; 7 after the dashboard edit lands
			if gets > 1 {
				revision = 7
			}
			io.WriteString(w, paywallResponseJSON("null", revision))
		case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/paywalls/pw_new"):
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			patched = append(patched, body)
			w.WriteHeader(http.StatusConflict)
			io.WriteString(w, `{"object":"error","type":"resource_conflict","message":"draft revision is stale"}`)
		default:
			t.Errorf("unexpected API request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(apiServer.Close)
	paywallAIServer, _ := stubEditorServer(t)
	t.Setenv("RC_BASE_URL", apiServer.URL)
	t.Setenv("RC_PAYWALL_AI_BASE_URL", paywallAIServer.URL)
	t.Setenv("RC_OFFERING_ID", "")

	stdout, _, err := runAgentCmd(t,
		"paywalls", "generate", "--name", "Summer sale",
		"--prompt", "A calm annual-first paywall",
		"--session", filepath.Join(t.TempDir(), "session.json"),
		"--project-id", "proj1",
		"--json", "--no-input", "--api-key", "sk_test",
	)
	if err != nil {
		t.Fatal(err)
	}
	if gets != 1 {
		t.Fatalf("GETs = %d, want 1 (create-time only; persist must not refetch)", gets)
	}
	if len(patched) != 1 || patched[0]["revision"] != 3.0 {
		t.Fatalf("patched = %v, want one PATCH carrying the create-time revision 3", patched)
	}
	if !strings.Contains(stdout, `"saved_to_draft": false`) {
		t.Fatalf("stdout = %s", stdout)
	}
}

// With no --session, the session file and its screenshots land under the CLI
// data dir (paywalls/<project>/<paywall>/), and every surfaced path is absolute.
func TestPaywallsGenerate_DefaultsToDataDir(t *testing.T) {
	apiURL, paywallAIURL, _, _ := paywallAITestServers(t, "ofrng_default")
	t.Setenv("RC_BASE_URL", apiURL)
	t.Setenv("RC_PAYWALL_AI_BASE_URL", paywallAIURL)

	stdout, _, err := runAgentCmd(t,
		"paywalls", "generate", "--offering-id", "ofrng_default",
		"--prompt", "A calm annual-first paywall",
		"--project-id", "proj1",
		"--json", "--no-input", "--api-key", "sk_test",
	)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			SessionFile     string            `json:"session_file"`
			ScreenshotPaths map[string]string `json:"screenshot_paths"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, stdout)
	}
	sf := envelope.Data.SessionFile
	if !filepath.IsAbs(sf) {
		t.Fatalf("session_file must be absolute, got %q", sf)
	}
	if !strings.Contains(sf, filepath.Join("paywalls", "proj1", "pw_new")) || filepath.Base(sf) != "session.paywall.json" {
		t.Fatalf("session_file not centralized per project/paywall: %q", sf)
	}
	if _, err := os.Stat(sf); err != nil {
		t.Fatalf("session file not written: %v", err)
	}
	shot := envelope.Data.ScreenshotPaths["light"]
	if !filepath.IsAbs(shot) || filepath.Base(shot) != "session.light.png" {
		t.Fatalf("screenshot must be absolute and beside the session, got %q", shot)
	}
	if _, err := os.Stat(shot); err != nil {
		t.Fatalf("screenshot not written: %v", err)
	}
}

// An offering can only have one paywall, which the server enforces with a 409.
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
	if err == nil || !strings.Contains(err.Error(), "ofrng_default already has a paywall") {
		t.Fatalf("err = %v", err)
	}
	var hinted interface{ Hint() string }
	if !errors.As(err, &hinted) || !strings.Contains(hinted.Hint(), "omit --offering-id") {
		t.Fatalf("recovery hint not attached to error: %v", err)
	}
}

// staleTestSession is a session file whose stored revision the tests pit
// against the API stub's current draft revision.
const staleTestSession = `{
  "version": 1,
  "project_id": "proj1",
  "paywall_id": "pw_new",
  "session_id": "sess1",
  "revision": 3,
  "paywall": {"default_locale": "en_US", "offering_id": null, "components_config": {"stack": true}, "components_localizations": {"en_US": {}}},
  "ui_config": {"fonts": {}, "presets": {"saved_colors": []}},
  "product_variables": {},
  "__unstable_session_items": [{"k": 2}]
}`

// A session whose revision diverged from the server must stop at preflight:
// no design turn is spent, and without --yes the command errors instead of
// silently starting over from the server's draft.
func TestPaywallsEdit_StaleSessionStopsBeforeDesignTurn(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/paywalls/pw_new") {
			t.Errorf("unexpected API request %s %s", r.Method, r.URL.Path)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, paywallResponseJSON("null", 5))
	}))
	t.Cleanup(apiServer.Close)
	paywallAIServer, paywallAIRequests := stubEditorServer(t)
	t.Setenv("RC_BASE_URL", apiServer.URL)
	t.Setenv("RC_PAYWALL_AI_BASE_URL", paywallAIServer.URL)

	sessionPath := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(sessionPath, []byte(staleTestSession), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := runAgentCmd(t,
		"paywalls", "edit",
		"--session", sessionPath,
		"--prompt", "Push the gradient harder",
		"--no-input", "--api-key", "sk_test",
	)
	if err == nil || !strings.Contains(err.Error(), "pass --yes") {
		t.Fatalf("err = %v", err)
	}
	if *paywallAIRequests != 0 {
		t.Fatalf("editor requests = %d, want 0 (no design turn on a stale session)", *paywallAIRequests)
	}
	payload, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != staleTestSession {
		t.Fatalf("session file was modified:\n%s", payload)
	}
}

// The PATCH must carry the session's own revision — not a freshly fetched
// one — or the backend's conflict guard can never fire for a stale session. The
// single GET is the preflight; persist itself must not refetch.
func TestPaywallsEdit_PatchCarriesSessionRevision(t *testing.T) {
	gets := 0
	var patched []map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/paywalls/pw_new"):
			gets++
			io.WriteString(w, paywallResponseJSON("null", 3))
		case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/paywalls/pw_new"):
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			patched = append(patched, body)
			io.WriteString(w, paywallResponseJSON("null", 4))
		default:
			t.Errorf("unexpected API request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(apiServer.Close)
	paywallAIServer, _ := stubEditorServer(t)
	t.Setenv("RC_BASE_URL", apiServer.URL)
	t.Setenv("RC_PAYWALL_AI_BASE_URL", paywallAIServer.URL)

	sessionPath := filepath.Join(t.TempDir(), "session.json")
	if err := os.WriteFile(sessionPath, []byte(staleTestSession), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := runAgentCmd(t,
		"paywalls", "edit",
		"--session", sessionPath,
		"--prompt", "Push the gradient harder",
		"--json", "--no-input", "--api-key", "sk_test",
	)
	if err != nil {
		t.Fatal(err)
	}
	if gets != 1 {
		t.Fatalf("GETs = %d, want 1 (preflight only; persist must not refetch)", gets)
	}
	if len(patched) != 1 || patched[0]["revision"] != 3.0 {
		t.Fatalf("patched = %v", patched)
	}
}

func TestPaywallsEdit_RequiresSessionOrID(t *testing.T) {
	_, _, err := runCmd(t, "paywalls", "edit", "--prompt", "x", "--no-input", "--api-key", "sk_test")
	if err == nil || !strings.Contains(err.Error(), "pass a paywall ID or --session") {
		t.Fatalf("err = %v", err)
	}
}

func TestPaywallsGenerate_MidStreamDropCheckpoints(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/paywalls"):
			io.WriteString(w, `{"id":"pw_new","offering_id":"ofrng_default","created_at":1720000000000,"published_at":null}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/paywalls/pw_new"):
			io.WriteString(w, paywallResponseJSON(`"ofrng_default"`, 3))
		default:
			t.Errorf("unexpected API request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(apiServer.Close)

	// Editor streams one snapshot, then closes without run.completed (a drop).
	paywallAIServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: {\"type\":\"run.started\",\"session_id\":\"sess1\"}\n\n")
		io.WriteString(w, "data: {\"type\":\"turn.snapshot\",\"session_id\":\"sess1\",\"paywall\":{\"default_locale\":\"en_US\",\"offering_id\":null,\"components_config\":{\"stack\":true},\"components_localizations\":{\"en_US\":{}}},\"activity\":[{\"id\":\"a1\",\"type\":\"tool\",\"tool_name\":\"edit_components\",\"status\":\"success\",\"display\":{\"text\":\"Built hero\"}}],\"__unstable_session_items\":[{\"k\":1}]}\n\n")
	}))
	t.Cleanup(paywallAIServer.Close)

	t.Setenv("RC_BASE_URL", apiServer.URL)
	t.Setenv("RC_PAYWALL_AI_BASE_URL", paywallAIServer.URL)

	sessionPath := filepath.Join(t.TempDir(), "session.json")
	_, _, err := runAgentCmd(t,
		"paywalls", "generate", "--offering-id", "ofrng_default", "--name", "Drop test",
		"--prompt", "a paywall", "--session", sessionPath, "--project-id", "proj1",
		"--no-input", "--api-key", "sk_test",
	)
	if err == nil || !strings.Contains(err.Error(), "stream ended before the run finished") {
		t.Fatalf("want mid-stream drop error, got %v", err)
	}

	payload, rerr := os.ReadFile(sessionPath)
	if rerr != nil {
		t.Fatalf("session not checkpointed: %v", rerr)
	}
	var session map[string]any
	if err := json.Unmarshal(payload, &session); err != nil {
		t.Fatal(err)
	}
	if session["paywall_id"] != "pw_new" {
		t.Fatalf("session paywall_id = %v", session["paywall_id"])
	}
	items, ok := session["__unstable_session_items"].([]any)
	if !ok || len(items) == 0 || items[0].(map[string]any)["k"] != 1.0 {
		t.Fatalf("snapshot not checkpointed: %v", session["__unstable_session_items"])
	}
}
