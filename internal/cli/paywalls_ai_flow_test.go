package cli_test

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/cli"
)

const flowDraftJSON = `{"id":"wf_1","object":"flow","revision":4,"type":"paywall","steps":{` +
	`"screen":{"type":"screen","name":"Main","screen":{"offering_id":"ofrng1","components_config":{"old":true},"components_localizations":{},"haptics_enabled":false}},` +
	`"other":{"type":"screen","screen":{"components_config":{"keep":true}}},` +
	`"auth":{"type":"auth"}}}`

const (
	streamStarted    = "data: {\"type\":\"run.started\",\"session_id\":\"sess1\"}\n\n"
	streamEdited     = "data: {\"type\":\"run.completed\",\"session_id\":\"sess1\",\"edited_paywalls_by_step_id\":{\"screen\":{\"default_locale\":\"en_US\",\"offering_id\":null,\"components_config\":{\"new\":true},\"components_localizations\":{\"en_US\":{}}}}}\n\n"
	streamEditedFull = "data: {\"type\":\"run.completed\",\"session_id\":\"sess1\",\"edited_paywalls_by_step_id\":{" +
		"\"screen\":{\"default_locale\":\"en_US\",\"components_config\":{\"a\":1},\"components_localizations\":{},\"state_declarations\":{\"x\":1},\"automatically_scale_font_size\":false}," +
		"\"other\":{\"default_locale\":\"fr_FR\",\"state_declarations\":null,\"components_config\":{\"b\":2},\"components_localizations\":{}}}}\n\n"
	streamSiblingIssues = "data: {\"type\":\"run.completed\",\"session_id\":\"sess1\",\"edited_paywalls_by_step_id\":{" +
		"\"screen\":{\"default_locale\":\"en_US\",\"components_config\":{},\"components_localizations\":{}}," +
		"\"other\":{\"default_locale\":\"en_US\",\"components_config\":{},\"components_localizations\":{}}}," +
		"\"validation_issues\":[{\"key\":\"other.cta\",\"message\":\"no action\"}]}\n\n"
	streamNoEdits = "data: {\"type\":\"run.completed\",\"session_id\":\"sess1\",\"edited_paywalls_by_step_id\":{}}\n\n"
	streamFailed  = "data: {\"type\":\"run.failed\",\"session_id\":\"sess1\",\"error\":{\"code\":\"internal\",\"message\":\"boom\"},\"edited_paywalls_by_step_id\":{\"screen\":{\"default_locale\":\"en_US\",\"components_config\":{\"partial\":true},\"components_localizations\":{}}}}\n\n"
)

func TestFlowsEdit(t *testing.T) {
	tests := []struct {
		name        string
		draft       string
		args        []string
		stream      string
		saveStatus  int
		wantErr     string
		wantStream  bool
		wantPatches int
		wantOut     string
		wantPatch   string
		streamCode  int
		wantRefs    []string
		wantSession string
	}{
		{
			name:        "saves the edited step against the draft revision",
			args:        []string{"wf_1", "--step-ids", "screen"},
			stream:      streamStarted + streamEdited,
			saveStatus:  http.StatusOK,
			wantStream:  true,
			wantPatches: 1,
			wantOut:     "saved_to_draft",
			wantPatch:   `{"revision":4,"updated_steps":{"screen":{"screen":{"default_locale":"en_US","components_config":{"new":true},"components_localizations":{"en_US":{}}}}}}`,
		},
		{
			name:        "every edited step sends only its screen",
			args:        []string{"wf_1", "--step-ids", "screen"},
			stream:      streamStarted + streamEditedFull,
			saveStatus:  http.StatusOK,
			wantStream:  true,
			wantPatches: 1,
			wantOut:     "saved_to_draft",
			wantPatch: `{"revision":4,"updated_steps":{` +
				`"screen":{"screen":{"default_locale":"en_US","components_config":{"a":1},"components_localizations":{},"state_declarations":{"x":1},"automatically_scale_font_size":false}},` +
				`"other":{"screen":{"default_locale":"fr_FR","components_config":{"b":2},"components_localizations":{}}}}}`,
		},
		{
			name:        "no edits means no save",
			args:        []string{"wf_1", "--step-ids", "screen"},
			stream:      streamStarted + streamNoEdits,
			wantStream:  true,
			wantPatches: 0,
			wantOut:     `"saved_to_draft": false`,
		},
		{
			name:        "failed run saves nothing",
			args:        []string{"wf_1", "--step-ids", "screen"},
			stream:      streamStarted + streamFailed,
			wantStream:  true,
			wantErr:     "boom",
			wantPatches: 0,
		},
		{
			name:        "revision conflict is reported and not retried",
			args:        []string{"wf_1", "--step-ids", "screen"},
			stream:      streamStarted + streamEdited,
			saveStatus:  http.StatusConflict,
			wantStream:  true,
			wantPatches: 1,
			wantOut:     `"saved_to_draft": false`,
		},
		{
			name:        "failed save returns the error",
			args:        []string{"wf_1", "--step-ids", "screen"},
			stream:      streamStarted + streamEdited,
			saveStatus:  http.StatusForbidden,
			wantStream:  true,
			wantErr:     "403",
			wantPatches: 1,
		},
		{
			name:        "repeated --step-ids sends every id to the editor",
			args:        []string{"wf_1", "--step-ids", "screen,other", "--step-ids", "screen"},
			stream:      streamStarted + streamEditedFull,
			saveStatus:  http.StatusOK,
			wantStream:  true,
			wantPatches: 1,
			wantOut:     `"step_ids": [`,
			wantRefs:    []string{"screen", "other"},
			wantSession: "/wf_1/session.paywall.json",
		},
		{
			name:    "every --step-ids must exist",
			args:    []string{"wf_1", "--step-ids", "screen", "--step-ids", "nope"},
			wantErr: `no step "nope"`,
		},
		{
			name:    "every --step-ids must be a screen",
			args:    []string{"wf_1", "--step-ids", "screen", "--step-ids", "auth"},
			wantErr: "only screens can be edited",
		},
		{
			name:       "editor rejection message is shown",
			args:       []string{"wf_1", "--step-ids", "screen", "--step-ids", "other"},
			streamCode: http.StatusUnprocessableEntity,
			stream:     `{"detail":"only one referenced step is supported"}`,
			wantStream: true,
			wantErr:    "only one referenced step is supported",
		},
		{
			name:    "session file is rejected",
			args:    []string{"wf_1", "--step-ids", "screen", "--session", "x.paywall.json"},
			wantErr: "drop --session",
		},
		{
			name:        "omitted --step-ids sends an empty list",
			args:        []string{"wf_1"},
			stream:      streamStarted + streamEdited,
			saveStatus:  http.StatusOK,
			wantStream:  true,
			wantPatches: 1,
			wantOut:     `"step_ids": []`,
			wantRefs:    []string{},
			wantSession: "/wf_1/session.paywall.json",
		},
		{
			name:    "flow id is required without a prompt for it",
			args:    []string{"--step-ids", "screen"},
			wantErr: "flow ID is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var streamed bool
			var patchBody []byte
			var patches int
			var streamBody map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/projects/proj/flows/wf_1":
					_, _ = io.WriteString(w, cmp.Or(tt.draft, flowDraftJSON))
				case r.Method == http.MethodPost && r.URL.Path == "/editor/v3/stream":
					streamed = true
					_ = json.NewDecoder(r.Body).Decode(&streamBody)
					if tt.streamCode != 0 {
						w.WriteHeader(tt.streamCode)
					}
					_, _ = io.WriteString(w, tt.stream)
				case r.Method == http.MethodPatch && r.URL.Path == "/projects/proj/flows/wf_1":
					patchBody, _ = io.ReadAll(r.Body)
					patches++
					w.WriteHeader(tt.saveStatus)
					_, _ = io.WriteString(w, `{"id":"wf_1","revision":5}`)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			args := append([]string{"flows", "edit"}, tt.args...)
			args = append(args, "--prompt", "make it blue", "--base-url", server.URL, "--json", "--no-input")
			out, _, err := runProjectSetupCommand(t, server.URL, args...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if streamed != tt.wantStream {
				t.Fatalf("streamed = %v, want %v", streamed, tt.wantStream)
			}
			if patches != tt.wantPatches {
				t.Fatalf("patches = %d, want %d", patches, tt.wantPatches)
			}
			if tt.wantOut != "" && !strings.Contains(out, tt.wantOut) {
				t.Fatalf("output missing %q: %s", tt.wantOut, out)
			}
			if tt.wantSession != "" && !strings.Contains(out, tt.wantSession+`"`) {
				t.Fatalf("session_file missing %q: %s", tt.wantSession, out)
			}
			if tt.wantRefs != nil {
				ctx, _ := streamBody["workflow_context"].(map[string]any)
				got, _ := json.Marshal(ctx["referenced_step_ids"])
				want, _ := json.Marshal(tt.wantRefs)
				if string(got) != string(want) {
					t.Fatalf("referenced_step_ids = %s, want %s", got, want)
				}
			}
			if tt.wantStream {
				if ctx, _ := streamBody["workflow_context"].(map[string]any); ctx["workflow_revision"] != float64(4) {
					t.Fatalf("workflow_context = %v", streamBody["workflow_context"])
				}
			}
			if tt.wantPatch != "" {
				var want, got any
				if err := json.Unmarshal([]byte(tt.wantPatch), &want); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(patchBody, &got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("PATCH body = %s\nwant %s", patchBody, tt.wantPatch)
				}
			}
		})
	}
}

func flowEditServer(t *testing.T, stream string, saveStatus int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, flowDraftJSON)
		case r.URL.Path == "/editor/v3/stream":
			_, _ = io.WriteString(w, stream)
		default:
			w.WriteHeader(saveStatus)
			_, _ = io.WriteString(w, `{"id":"wf_1","revision":5}`)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestFlowsEditJSONShape(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		stream     string
		saveStatus int
		want       string
	}{
		{
			name:       "sibling edit and validation issues",
			args:       []string{"--step-ids", "screen"},
			stream:     streamStarted + streamSiblingIssues,
			saveStatus: http.StatusOK,
			want:       `{"step_ids":["screen"],"saved_steps":["other","screen"],"also_changed":["other"],"validation_issues":[{"key":"other.cta","message":"no action"}],"saved_to_draft":true}`,
		},
		{
			name:       "whole-flow run has no sibling steps",
			stream:     streamStarted + streamSiblingIssues,
			saveStatus: http.StatusOK,
			want:       `{"step_ids":[],"saved_steps":["other","screen"],"also_changed":[],"saved_to_draft":true}`,
		},
		{
			name:       "stale revision saves nothing but still reports the arrays",
			args:       []string{"--step-ids", "screen"},
			stream:     streamStarted + streamSiblingIssues,
			saveStatus: http.StatusConflict,
			want:       `{"step_ids":["screen"],"saved_steps":[],"also_changed":["other"],"validation_issues":[{"key":"other.cta","message":"no action"}],"saved_to_draft":false}`,
		},
		{
			name:       "no edits and no issues",
			stream:     streamStarted + streamNoEdits,
			saveStatus: http.StatusOK,
			want:       `{"saved_steps":[],"also_changed":[],"validation_issues":[],"saved_to_draft":false}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := flowEditServer(t, tt.stream, tt.saveStatus)
			args := append([]string{"flows", "edit", "wf_1"}, tt.args...)
			args = append(args, "--prompt", "x", "--base-url", server.URL, "--json", "--no-input")
			out, _, err := runProjectSetupCommand(t, server.URL, args...)
			if err != nil {
				t.Fatal(err)
			}
			var envelope struct {
				Data map[string]any `json:"data"`
			}
			var want map[string]any
			if err := json.Unmarshal([]byte(out), &envelope); err != nil {
				t.Fatalf("output is not JSON: %v\n%s", err, out)
			}
			if err := json.Unmarshal([]byte(tt.want), &want); err != nil {
				t.Fatal(err)
			}
			for key, w := range want {
				if !reflect.DeepEqual(envelope.Data[key], w) {
					t.Errorf("%s = %v, want %v", key, envelope.Data[key], w)
				}
			}
		})
	}
}

func TestFlowsEditSessionIsPerFlow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, flowDraftJSON)
		case r.URL.Path == "/editor/v3/stream":
			_, _ = io.WriteString(w, streamStarted+streamEdited)
		default:
			_, _ = io.WriteString(w, `{"id":"wf","revision":5}`)
		}
	}))
	defer server.Close()
	sessionFile := func(args ...string) string {
		t.Helper()
		args = append([]string{"flows", "edit"}, args...)
		args = append(args, "--prompt", "x", "--base-url", server.URL, "--json", "--no-input")
		out, _, err := runProjectSetupCommand(t, server.URL, args...)
		if err != nil {
			t.Fatal(err)
		}
		var envelope struct {
			Data struct {
				SessionFile string `json:"session_file"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(out), &envelope); err != nil {
			t.Fatalf("output is not JSON: %v\n%s", err, out)
		}
		// Each run gets a fresh config dir; compare the path beneath it.
		_, rel, _ := strings.Cut(envelope.Data.SessionFile, "paywalls")
		return rel
	}
	first := sessionFile("wf_1", "--step-ids", "screen")
	if got := sessionFile("wf_1", "--step-ids", "other"); got != first {
		t.Errorf("different --step-ids on one flow: session %s, want %s", got, first)
	}
	if got := sessionFile("wf_1"); got != first {
		t.Errorf("whole-flow run: session %s, want %s", got, first)
	}
	if got := sessionFile("wf_2", "--step-ids", "screen"); got == first {
		t.Errorf("different flows share session %s", got)
	}
}

func TestFlowsEditRemovesStalePreviews(t *testing.T) {
	server := flowEditServer(t, streamStarted+streamEdited, http.StatusOK)
	configDir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", configDir)
	t.Setenv("RC_BASE_URL", server.URL)
	dir := filepath.Join(configDir, "paywalls", "proj", "wf_1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := []string{filepath.Join(dir, "session.screen.light.png"), filepath.Join(dir, "session.screen.dark.png")}
	for _, path := range stale {
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	root := cli.NewRootCmd("test")
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"flows", "edit", "wf_1", "--step-ids", "screen", "--prompt", "x", "--base-url", server.URL, "--no-input", "--api-key", "sk_test", "--project-id", "proj"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, path := range stale {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("stale preview %s survived the run (stat err = %v)", path, err)
		}
	}
	if !strings.Contains(out.String(), "No preview was returned") {
		t.Errorf("output missing the no-preview note: %s", out.String())
	}
}
