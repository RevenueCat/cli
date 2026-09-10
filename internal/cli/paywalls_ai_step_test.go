package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/output"
)

// rcGraphMock serves a two-screen graph (a terminal, non-fallback step_1 and
// a fallback step_2) plus a PATCH endpoint that records the query string and
// body it received. The plain paywall GET serves only the parent's offering;
// it fails the test if asked to expand components, since a sibling's content
// is reachable only through the graph.
type rcGraphMock struct {
	mu           sync.Mutex
	patchedQuery []string
	patchedBody  []map[string]any
	// patchStatus, when non-zero, makes the PATCH endpoint fail with this
	// status instead of succeeding.
	patchStatus int
}

func (m *rcGraphMock) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/graph"):
			io.WriteString(w, `{"object":"paywall_graph","id":"pw_parent","version":"draft","graph":{
				"revision":9,"initial_step_id":"step_1","paywall_step_id":"step_1","total_steps":2,
				"steps":[
					{"id":"step_1","name":"Purchase","type":"screen","screen_types":["paywall"],"is_terminal":true,"paywall_id":"pw_parent","edges":[],"unwired_triggers":[]},
					{"id":"step_2","name":"Intro","type":"screen","screen_types":["generic"],"is_terminal":false,"paywall_id":"pw_sibling","edges":[],"unwired_triggers":[],
					 "paywall":{"id":"pw_sibling","revision":5,"components_config":{"base":{}},"components_localizations":{"en_US":{}},"default_locale":"en_US","state_declarations":{}}}
				]}}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/paywalls/pw_parent"):
			if r.URL.Query().Has("expand") {
				t.Errorf("parent GET must not expand components: %s", r.URL.RawQuery)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			io.WriteString(w, `{"id":"pw_parent","offering_id":"ofrng_parent","created_at":1}`)
		case r.Method == http.MethodPatch:
			m.mu.Lock()
			m.patchedQuery = append(m.patchedQuery, r.URL.RawQuery)
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			m.patchedBody = append(m.patchedBody, body)
			status := m.patchStatus
			m.mu.Unlock()
			if status != 0 {
				w.WriteHeader(status)
				io.WriteString(w, `{"type":"conflict","message":"draft changed"}`)
				return
			}
			// The response id is the sibling's own canonical id — different
			// from pw_parent, the path/parent id.
			io.WriteString(w, `{"id":"pw_sibling","offering_id":"","created_at":1,"components":{"published":null,"draft":{"revision":6,"components_config":{"base":{}},"components_localizations":{"en_US":{}},"default_locale":"en_US"}}}`)
		default:
			t.Errorf("unexpected request that should have gone through the graph instead: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// stepEchoEditorServer records the step_id each editor request carried and
// completes the turn immediately.
type stepEchoEditorServer struct {
	mu               sync.Mutex
	stepIDs          []string
	componentsConfig []string
	offeringIDs      []string
}

func (s *stepEchoEditorServer) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			StepID  *string `json:"step_id"`
			Paywall struct {
				ComponentsConfig json.RawMessage `json:"components_config"`
				OfferingID       *string         `json:"offering_id"`
			} `json:"paywall"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		if body.StepID != nil {
			s.stepIDs = append(s.stepIDs, *body.StepID)
		} else {
			s.stepIDs = append(s.stepIDs, "")
		}
		if body.Paywall.OfferingID != nil {
			s.offeringIDs = append(s.offeringIDs, *body.Paywall.OfferingID)
		} else {
			s.offeringIDs = append(s.offeringIDs, "")
		}
		s.componentsConfig = append(s.componentsConfig, string(body.Paywall.ComponentsConfig))
		s.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		paywall := `{"default_locale":"en_US","offering_id":null,"components_config":{"designed":true},"components_localizations":{"en_US":{}}}`
		fmt.Fprint(w, "data: {\"type\":\"run.started\",\"session_id\":\"sess1\"}\n\n")
		fmt.Fprintf(w, "data: {\"type\":\"turn.snapshot\",\"session_id\":\"sess1\",\"turn_index\":0,\"paywall\":%s,\"activity\":[]}\n\n", paywall)
		fmt.Fprintf(w, "data: {\"type\":\"run.completed\",\"session_id\":\"sess1\",\"trace_id\":\"tr1\",\"paywall\":%s,\"activity\":[]}\n\n", paywall)
	}))
}

func TestPaywallsEdit_StepIDFetchesFromGraphAndSavesToSelectedStep(t *testing.T) {
	rc := &rcGraphMock{}
	rcServer := rc.server(t)
	defer rcServer.Close()
	editor := &stepEchoEditorServer{}
	editorServer := editor.server(t)
	defer editorServer.Close()

	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	t.Setenv("RC_PAYWALL_AI_BASE_URL", editorServer.URL)
	cmd := newPaywallsEditCmd()
	var stdout bytes.Buffer
	rt := &Runtime{
		Globals: &Globals{JSON: true, NoInput: true, Version: "test"},
		Config:  &config.Config{APIKey: "sk_test", ProjectID: "proj", BaseURL: rcServer.URL},
		Ctx:     context.Background(),
		Out:     output.NewRenderer(&stdout, io.Discard, true, true, false, ""),
	}
	cmd.SetContext(WithRuntime(context.Background(), rt))
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"pw_parent", "--step-id", "step_2", "--prompt", "make it pop"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("edit turn failed: %v, stdout=%s", err, stdout.String())
	}

	editor.mu.Lock()
	stepIDs := append([]string(nil), editor.stepIDs...)
	editor.mu.Unlock()
	if len(stepIDs) != 1 || stepIDs[0] != "step_2" {
		t.Fatalf("EditorRequest.step_id = %v, want [step_2]", stepIDs)
	}

	editor.mu.Lock()
	sentConfig := append([]string(nil), editor.componentsConfig...)
	editor.mu.Unlock()
	if len(sentConfig) != 1 || sentConfig[0] != `{"base":{}}` {
		t.Fatalf("editor received components_config = %v, want the sibling step_2's own content", sentConfig)
	}

	editor.mu.Lock()
	sentOfferings := append([]string(nil), editor.offeringIDs...)
	editor.mu.Unlock()
	if len(sentOfferings) != 1 || sentOfferings[0] != "ofrng_parent" {
		t.Fatalf("editor received offering_id = %v, want the parent's ofrng_parent so products resolve", sentOfferings)
	}

	rc.mu.Lock()
	queries := append([]string(nil), rc.patchedQuery...)
	rc.mu.Unlock()
	if len(queries) != 1 || queries[0] != "step_id=step_2" {
		t.Fatalf("PATCH query = %v, want [step_id=step_2] against the parent path", queries)
	}

	var envelope struct {
		Data struct {
			PaywallID string `json:"paywall_id"`
			TargetID  string `json:"target_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("decoding output: %v\n%s", err, stdout.String())
	}
	if envelope.Data.PaywallID != "pw_parent" {
		t.Fatalf("paywall_id = %q, want the parent pw_parent", envelope.Data.PaywallID)
	}
	if envelope.Data.TargetID != "pw_sibling" {
		t.Fatalf("target_id = %q, want the PATCH response's own id pw_sibling, not the pre-edit parent id", envelope.Data.TargetID)
	}
}

// The recovery hint on a failed save must point back at the selected screen,
// not at the paywall's fallback — running the bare hint command would
// otherwise silently reseed and edit the purchase screen instead.
func TestPaywallsEdit_StepIDConflictHintKeepsStepSelection(t *testing.T) {
	rc := &rcGraphMock{patchStatus: http.StatusConflict}
	rcServer := rc.server(t)
	defer rcServer.Close()
	editor := &stepEchoEditorServer{}
	editorServer := editor.server(t)
	defer editorServer.Close()

	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	t.Setenv("RC_PAYWALL_AI_BASE_URL", editorServer.URL)
	cmd := newPaywallsEditCmd()
	var stdout, stderr bytes.Buffer
	rt := &Runtime{
		Globals: &Globals{NoInput: true, Version: "test"},
		Config:  &config.Config{APIKey: "sk_test", ProjectID: "proj", BaseURL: rcServer.URL},
		Ctx:     context.Background(),
		Out:     output.NewRenderer(&stdout, &stderr, false, true, false, ""),
	}
	cmd.SetContext(WithRuntime(context.Background(), rt))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"pw_parent", "--step-id", "step_2", "--prompt", "make it pop"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("edit turn failed: %v, stderr=%s", err, stderr.String())
	}

	if !strings.Contains(stderr.String(), "rc paywalls edit pw_parent --step-id step_2") {
		t.Fatalf("hint must re-run with the same --step-id, got stderr:\n%s", stderr.String())
	}
}

// An unresolvable --step-id must fail clearly and must never silently
// retarget the purchase/fallback screen.
func TestSeedSessionFromServerForStep_UnknownStepFailsClearly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"object":"paywall_graph","id":"pw","version":"draft","graph":{
			"revision":1,"initial_step_id":"step_1","paywall_step_id":"step_1","total_steps":1,
			"steps":[{"id":"step_1","name":"Purchase","type":"screen","screen_types":["paywall"],"is_terminal":true,"paywall_id":"pw","edges":[],"unwired_triggers":[],
			 "paywall":{"id":"pw","revision":1,"components_config":{},"components_localizations":{},"default_locale":"en_US","state_declarations":{}}}]}}`)
	}))
	defer server.Close()

	rt := newSessionTestRuntime(server.URL)
	session, err := seedSessionFromServerForStep(context.Background(), rt, "proj", "pw", "step_does_not_exist")
	if err == nil {
		t.Fatalf("expected an error, got session %+v", session)
	}
	if !strings.Contains(err.Error(), "step_does_not_exist") {
		t.Fatalf("error should name the unresolvable step id: %v", err)
	}
}

// A standalone paywall (graph: null) must reject --step-id rather than
// silently editing the fallback as if no step had been selected.
func TestSeedSessionFromServerForStep_StandalonePaywallFailsClearly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"object":"paywall_graph","id":"pw_solo","version":"draft","graph":null}`)
	}))
	defer server.Close()

	rt := newSessionTestRuntime(server.URL)
	session, err := seedSessionFromServerForStep(context.Background(), rt, "proj", "pw_solo", "step_1")
	if err == nil {
		t.Fatalf("expected an error, got session %+v", session)
	}
	if !strings.Contains(err.Error(), "standalone") {
		t.Fatalf("error should explain the paywall is standalone: %v", err)
	}
}

// --step-id combined with --session is rejected outright rather than
// guessing which selection wins.
func TestPaywallsEdit_StepIDConflictsWithSession(t *testing.T) {
	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	cmd := newPaywallsEditCmd()
	rt := newSessionTestRuntime("https://rc.invalid")
	cmd.SetContext(WithRuntime(context.Background(), rt))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--session", "some-file.json", "--step-id", "step_2", "--prompt", "x"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--step-id") {
		t.Fatalf("err = %v, want a clear rejection of --step-id with --session", err)
	}
}
