package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/paywallai"
)

func newSessionTestRuntime(rcBaseURL string) *Runtime {
	return &Runtime{
		Globals: &Globals{JSON: true, NoInput: true, Version: "test"},
		Config:  &config.Config{APIKey: "sk_test", ProjectID: "proj", BaseURL: rcBaseURL},
		Ctx:     context.Background(),
		Out:     output.NewRenderer(io.Discard, io.Discard, true, true, false, ""),
	}
}

func newTestSession() *paywallAISession {
	rev := 0
	return &paywallAISession{
		Version:   1,
		ProjectID: "proj",
		PaywallID: "pw_test",
		Revision:  &rev,
		Paywall: paywallai.PaywallData{
			DefaultLocale:           "en_US",
			ComponentsConfig:        json.RawMessage(`{"base":{}}`),
			ComponentsLocalizations: json.RawMessage(`{"en_US":{}}`),
		},
		UIConfig:         json.RawMessage(minimalUIConfig),
		ProductVariables: map[string]string{},
		SessionItems:     json.RawMessage(`{}`),
	}
}

func failingEditorServer(t *testing.T, startedSessionID string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"type\":\"run.started\",\"session_id\":%q}\n\n", startedSessionID)
		fmt.Fprintf(w, "data: {\"type\":\"run.failed\",\"session_id\":%q,\"error\":{\"code\":\"internal\",\"message\":\"boom\"}}\n\n", startedSessionID)
	}))
}

func TestRunPaywallAI_FailedRunPersistsSessionID(t *testing.T) {
	editor := failingEditorServer(t, "sess_minted")
	defer editor.Close()

	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session"+paywallSessionSuffix)
	session := newTestSession()
	session.TraceID = "trace_prev"
	if err := savePaywallAISession(sessionPath, session); err != nil {
		t.Fatal(err)
	}

	rt := newSessionTestRuntime("https://rc.invalid")
	opts := paywallAIOptions{baseURL: editor.URL, sessionPath: sessionPath, prompt: "make it blue", timeout: 30 * time.Second}
	if err := runPaywallAI(context.Background(), rt, opts, session); err == nil {
		t.Fatal("expected the failed run to return an error")
	}

	stored, err := loadPaywallAISession(rt, sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SessionID != "sess_minted" {
		t.Fatalf("failed run did not persist session id: got %q, want sess_minted", stored.SessionID)
	}
	if stored.TraceID != "trace_prev" {
		t.Fatalf("failed run cleared the last good TraceID: got %q, want trace_prev", stored.TraceID)
	}
}

func TestRunPaywallAI_EmptyRunStartedKeepsStoredSessionID(t *testing.T) {
	editor := failingEditorServer(t, "")
	defer editor.Close()

	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session"+paywallSessionSuffix)
	session := newTestSession()
	session.SessionID = "sess_existing"
	if err := savePaywallAISession(sessionPath, session); err != nil {
		t.Fatal(err)
	}

	rt := newSessionTestRuntime("https://rc.invalid")
	opts := paywallAIOptions{baseURL: editor.URL, sessionPath: sessionPath, prompt: "keep going", timeout: 30 * time.Second}
	if err := runPaywallAI(context.Background(), rt, opts, session); err == nil {
		t.Fatal("expected the failed run to return an error")
	}

	stored, err := loadPaywallAISession(rt, sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SessionID != "sess_existing" {
		t.Fatalf("empty run_started wiped the stored session id: got %q, want sess_existing", stored.SessionID)
	}
}

type rcPaywallMock struct {
	mu                sync.Mutex
	revision          int
	stateDeclarations string // raw JSON served on the draft; omitted when empty
	patched           []map[string]any
}

func (m *rcPaywallMock) paywallJSON(rev int) string {
	declarations := ""
	if m.stateDeclarations != "" {
		declarations = `,"state_declarations":` + m.stateDeclarations
	}
	return fmt.Sprintf(`{"id":"pw_test","offering_id":"","created_at":1,"components":{"published":null,"draft":{"revision":%d,"components_config":{"base":{}},"components_localizations":{"en_US":{}},"default_locale":"en_US"%s}}}`, rev, declarations)
}

func (m *rcPaywallMock) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			m.mu.Lock()
			rev := m.revision
			m.mu.Unlock()
			fmt.Fprint(w, m.paywallJSON(rev))
		case http.MethodPatch:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			revision, _ := body["revision"].(float64)
			m.mu.Lock()
			m.patched = append(m.patched, body)
			m.revision = int(revision) + 1
			rev := m.revision
			m.mu.Unlock()
			fmt.Fprint(w, m.paywallJSON(rev))
		default:
			http.Error(w, "unexpected request", http.StatusMethodNotAllowed)
		}
	}))
}

func (m *rcPaywallMock) lastPatched(t *testing.T) map[string]any {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.patched) == 0 {
		t.Fatal("design was never PATCHed onto the draft")
	}
	return m.patched[len(m.patched)-1]
}

type echoEditorServer struct {
	mu                   sync.Mutex
	received             []string
	receivedDeclarations []string
	declarations         string // raw JSON echoed on the result paywall; omitted when empty
	minted               int
}

func (s *echoEditorServer) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SessionID string `json:"session_id"`
			Paywall   struct {
				StateDeclarations json.RawMessage `json:"state_declarations"`
			} `json:"paywall"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		s.received = append(s.received, body.SessionID)
		s.receivedDeclarations = append(s.receivedDeclarations, string(body.Paywall.StateDeclarations))
		sid := body.SessionID
		if sid == "" {
			s.minted++
			sid = fmt.Sprintf("sess_minted_%d", s.minted)
		}
		s.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		declarations := ""
		if s.declarations != "" {
			declarations = `,"state_declarations":` + s.declarations
		}
		paywall := `{"default_locale":"en_US","offering_id":null,"components_config":{"designed":true},"components_localizations":{"en_US":{}}` + declarations + `}`
		fmt.Fprintf(w, "data: {\"type\":\"run.started\",\"session_id\":%q}\n\n", sid)
		fmt.Fprintf(w, "data: {\"type\":\"turn.snapshot\",\"session_id\":%q,\"turn_index\":0,\"paywall\":%s,\"activity\":[]}\n\n", sid, paywall)
		fmt.Fprintf(w, "data: {\"type\":\"run.completed\",\"session_id\":%q,\"trace_id\":\"tr1\",\"paywall\":%s,\"activity\":[]}\n\n", sid, paywall)
	}))
}

func runEditTurn(t *testing.T, configDir, editorURL, rcURL, paywallID string) {
	t.Helper()
	t.Setenv("RC_CONFIG_DIR", configDir)
	t.Setenv("RC_PAYWALL_AI_BASE_URL", editorURL)
	cmd := newPaywallsEditCmd()
	rt := newSessionTestRuntime(rcURL)
	cmd.SetContext(WithRuntime(context.Background(), rt))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{paywallID, "--prompt", "make it pop"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("edit turn failed: %v", err)
	}
}

func TestPaywallsEdit_AutoContinuesSessionAcrossTurns(t *testing.T) {
	rc := &rcPaywallMock{}
	rcServer := rc.server(t)
	defer rcServer.Close()
	editor := &echoEditorServer{}
	editorServer := editor.server(t)
	defer editorServer.Close()

	configDir := t.TempDir()
	runEditTurn(t, configDir, editorServer.URL, rcServer.URL, "pw_test")
	runEditTurn(t, configDir, editorServer.URL, rcServer.URL, "pw_test")

	editor.mu.Lock()
	received := append([]string(nil), editor.received...)
	editor.mu.Unlock()

	if len(received) != 2 {
		t.Fatalf("expected 2 editor turns, got %d: %v", len(received), received)
	}
	if received[0] != "" {
		t.Fatalf("turn 1 should send an empty session id (fresh conversation), got %q", received[0])
	}
	if received[1] == "" {
		t.Fatal("turn 2 sent an empty session id — the conversation was not continued")
	}
	if received[1] != "sess_minted_1" {
		t.Fatalf("turn 2 should resend the id minted on turn 1 (sess_minted_1), got %q", received[1])
	}
}

func TestPaywallsEdit_ExplicitSessionOverridesDefault(t *testing.T) {
	rc := &rcPaywallMock{}
	rcServer := rc.server(t)
	defer rcServer.Close()
	editor := &echoEditorServer{}
	editorServer := editor.server(t)
	defer editorServer.Close()

	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "custom"+paywallSessionSuffix)
	stored := newTestSession()
	stored.SessionID = "sess_explicit"
	if err := savePaywallAISession(sessionPath, stored); err != nil {
		t.Fatal(err)
	}

	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	t.Setenv("RC_PAYWALL_AI_BASE_URL", editorServer.URL)
	cmd := newPaywallsEditCmd()
	rt := newSessionTestRuntime(rcServer.URL)
	cmd.SetContext(WithRuntime(context.Background(), rt))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--session", sessionPath, "--prompt", "tweak it"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("edit with --session failed: %v", err)
	}

	editor.mu.Lock()
	received := append([]string(nil), editor.received...)
	editor.mu.Unlock()
	if len(received) != 1 || received[0] != "sess_explicit" {
		t.Fatalf("--session did not drive the request session id: %v", received)
	}
}

func TestPaywallsEdit_RoundTripsStateDeclarations(t *testing.T) {
	fetched := `{"tab":{"type":"string","default":"a"}}`
	echoed := `{"tab":{"type":"string","default":"b"}}`
	rc := &rcPaywallMock{stateDeclarations: fetched}
	rcServer := rc.server(t)
	defer rcServer.Close()
	editor := &echoEditorServer{declarations: echoed}
	editorServer := editor.server(t)
	defer editorServer.Close()

	runEditTurn(t, t.TempDir(), editorServer.URL, rcServer.URL, "pw_test")

	editor.mu.Lock()
	sent := append([]string(nil), editor.receivedDeclarations...)
	editor.mu.Unlock()
	if len(sent) != 1 || sent[0] != fetched {
		t.Fatalf("editor request state_declarations = %v, want %s", sent, fetched)
	}

	var want any
	if err := json.Unmarshal([]byte(echoed), &want); err != nil {
		t.Fatal(err)
	}
	if got := rc.lastPatched(t)["state_declarations"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("PATCH state_declarations = %v, want %v", got, want)
	}
}

// A draft not written since before state declarations existed serves them as
// null; the CLI must send the editor an empty-but-present value (so it can
// wire new state) and must never PATCH an explicit null back (which clears).
func TestPaywallsEdit_NeverPatchesNullStateDeclarations(t *testing.T) {
	rc := &rcPaywallMock{stateDeclarations: "null"}
	rcServer := rc.server(t)
	defer rcServer.Close()
	editor := &echoEditorServer{} // an older editor that never echoes declarations
	editorServer := editor.server(t)
	defer editorServer.Close()

	runEditTurn(t, t.TempDir(), editorServer.URL, rcServer.URL, "pw_test")

	editor.mu.Lock()
	sent := append([]string(nil), editor.receivedDeclarations...)
	editor.mu.Unlock()
	if len(sent) != 1 || sent[0] != "{}" {
		t.Fatalf("editor request state_declarations = %v, want {}", sent)
	}

	got, ok := rc.lastPatched(t)["state_declarations"]
	if !ok || !reflect.DeepEqual(got, map[string]any{}) {
		t.Fatalf("PATCH state_declarations = %v (present: %v), want {}", got, ok)
	}
}
