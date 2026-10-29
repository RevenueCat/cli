package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const flowDraftJSON = `{"id":"wf_1","object":"flow_draft","revision":4,"type":"paywall","steps":{` +
	`"screen":{"type":"screen","name":"Main","paywall":{"offering_id":"ofrng1","components_config":{"old":true},"components_localizations":{},"haptics_enabled":false}},` +
	`"other":{"type":"screen","paywall":{"components_config":{"keep":true}}},` +
	`"auth":{"type":"auth"}}}`

const (
	streamStarted = "data: {\"type\":\"run.started\",\"session_id\":\"sess1\"}\n\n"
	streamEdited  = "data: {\"type\":\"run.completed\",\"session_id\":\"sess1\",\"edited_paywalls_by_step_id\":{\"screen\":{\"default_locale\":\"en_US\",\"offering_id\":null,\"components_config\":{\"new\":true},\"components_localizations\":{\"en_US\":{}}}}}\n\n"
	streamNoEdits = "data: {\"type\":\"run.completed\",\"session_id\":\"sess1\",\"edited_paywalls_by_step_id\":{}}\n\n"
	streamFailed  = "data: {\"type\":\"run.failed\",\"session_id\":\"sess1\",\"error\":{\"code\":\"internal\",\"message\":\"boom\"},\"edited_paywalls_by_step_id\":{\"screen\":{\"default_locale\":\"en_US\",\"components_config\":{\"partial\":true},\"components_localizations\":{}}}}\n\n"
)

func TestPaywallsEditStepID(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		stream      string
		saveStatus  int
		wantErr     string
		wantStream  bool
		wantPatches int
		wantOut     string
	}{
		{
			name:        "saves the edited step against the draft revision",
			args:        []string{"wf_1", "--step-id", "screen"},
			stream:      streamStarted + streamEdited,
			saveStatus:  http.StatusOK,
			wantStream:  true,
			wantPatches: 1,
			wantOut:     "saved_to_draft",
		},
		{
			name:        "no edits means no save",
			args:        []string{"wf_1", "--step-id", "screen"},
			stream:      streamStarted + streamNoEdits,
			wantStream:  true,
			wantPatches: 0,
			wantOut:     `"saved_to_draft": false`,
		},
		{
			name:        "failed run saves nothing",
			args:        []string{"wf_1", "--step-id", "screen"},
			stream:      streamStarted + streamFailed,
			wantStream:  true,
			wantErr:     "boom",
			wantPatches: 0,
		},
		{
			name:        "revision conflict is reported and not retried",
			args:        []string{"wf_1", "--step-id", "screen"},
			stream:      streamStarted + streamEdited,
			saveStatus:  http.StatusConflict,
			wantStream:  true,
			wantPatches: 1,
			wantOut:     `"saved_to_draft": false`,
		},
		{
			name:    "unknown step",
			args:    []string{"wf_1", "--step-id", "nope"},
			wantErr: `no step "nope"`,
		},
		{
			name:    "non-screen step",
			args:    []string{"wf_1", "--step-id", "auth"},
			wantErr: "only screens can be edited",
		},
		{
			name:    "session file is rejected with a step",
			args:    []string{"wf_1", "--step-id", "screen", "--session", "x.paywall.json"},
			wantErr: "drop --session",
		},
		{
			name:    "flow id is required",
			args:    []string{"--step-id", "screen"},
			wantErr: "needs the flow id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var streamed bool
			var patches []map[string]json.RawMessage
			var streamBody map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/projects/proj/flows/wf_1/draft":
					_, _ = io.WriteString(w, flowDraftJSON)
				case r.Method == http.MethodPost && r.URL.Path == "/editor/v3/stream":
					streamed = true
					_ = json.NewDecoder(r.Body).Decode(&streamBody)
					_, _ = io.WriteString(w, tt.stream)
				case r.Method == http.MethodPatch && r.URL.Path == "/projects/proj/flows/wf_1/draft":
					var body map[string]json.RawMessage
					_ = json.NewDecoder(r.Body).Decode(&body)
					patches = append(patches, body)
					w.WriteHeader(tt.saveStatus)
					_, _ = io.WriteString(w, `{"id":"wf_1","revision":5}`)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			args := append([]string{"paywalls", "edit"}, tt.args...)
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
			if len(patches) != tt.wantPatches {
				t.Fatalf("patches = %d, want %d", len(patches), tt.wantPatches)
			}
			if tt.wantOut != "" && !strings.Contains(out, tt.wantOut) {
				t.Fatalf("output missing %q: %s", tt.wantOut, out)
			}
			if tt.wantStream {
				if ctx, _ := streamBody["workflow_context"].(map[string]any); ctx["workflow_id"] != "wf_1" || ctx["focused_step_id"] != "screen" || ctx["workflow_revision"] != float64(4) {
					t.Fatalf("workflow_context = %v", streamBody["workflow_context"])
				}
				if _, ok := streamBody["paywall"]; ok {
					t.Fatal("v3 request carried a paywall")
				}
			}
			if len(patches) == 1 {
				patch := patches[0]
				if string(patch["revision"]) != "4" {
					t.Fatalf("revision = %s, want the draft revision read before the run", patch["revision"])
				}
				var updated map[string]map[string]json.RawMessage
				if err := json.Unmarshal(patch["updated_steps"], &updated); err != nil {
					t.Fatal(err)
				}
				if len(updated) != 1 {
					t.Fatalf("updated steps = %v, want only the edited step", updated)
				}
				step := updated["screen"]
				var paywall map[string]json.RawMessage
				if err := json.Unmarshal(step["paywall"], &paywall); err != nil {
					t.Fatal(err)
				}
				if string(step["name"]) != `"Main"` || string(paywall["offering_id"]) != `"ofrng1"` || string(paywall["haptics_enabled"]) != "false" {
					t.Fatalf("untouched fields lost: %s", step)
				}
				if string(paywall["components_config"]) != `{"new":true}` {
					t.Fatalf("components_config = %s", paywall["components_config"])
				}
			}
		})
	}
}

func TestPaywallsEditWithoutStepIDUsesV1(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		http.Error(w, "stop", http.StatusInternalServerError)
	}))
	defer server.Close()
	_, _, _ = runProjectSetupCommand(t, server.URL, "paywalls", "edit", "pw_1", "--prompt", "x", "--base-url", server.URL, "--json", "--no-input")
	for _, p := range paths {
		if strings.Contains(p, "/flows/") || strings.Contains(p, "/editor/v3/") {
			t.Fatalf("v1 edit touched flow path %s", p)
		}
	}
	if len(paths) == 0 {
		t.Fatal("no requests made")
	}
}
