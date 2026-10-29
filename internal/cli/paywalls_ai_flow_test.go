package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const flowDraftJSON = `{"id":"wf_1","object":"flow_draft","revision":4,"type":"paywall","steps":{` +
	`"screen":{"type":"screen","name":"Main","paywall":{"offering_id":"ofrng1","components_config":{"old":true},"components_localizations":{},"haptics_enabled":false}},` +
	`"other":{"type":"screen","paywall":{"components_config":{"keep":true}}},` +
	`"auth":{"type":"auth"}}}`

const (
	streamStarted    = "data: {\"type\":\"run.started\",\"session_id\":\"sess1\"}\n\n"
	streamEdited     = "data: {\"type\":\"run.completed\",\"session_id\":\"sess1\",\"edited_paywalls_by_step_id\":{\"screen\":{\"default_locale\":\"en_US\",\"offering_id\":null,\"components_config\":{\"new\":true},\"components_localizations\":{\"en_US\":{}}}}}\n\n"
	streamEditedFull = "data: {\"type\":\"run.completed\",\"session_id\":\"sess1\",\"edited_paywalls_by_step_id\":{" +
		"\"screen\":{\"default_locale\":\"en_US\",\"components_config\":{\"a\":1},\"components_localizations\":{},\"state_declarations\":{\"x\":1},\"automatically_scale_font_size\":false}," +
		"\"other\":{\"default_locale\":\"fr_FR\",\"state_declarations\":null,\"components_config\":{\"b\":2},\"components_localizations\":{}}}}\n\n"
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
		wantPatch   string
	}{
		{
			name:        "saves the edited step against the draft revision",
			args:        []string{"wf_1", "--step-id", "screen"},
			stream:      streamStarted + streamEdited,
			saveStatus:  http.StatusOK,
			wantStream:  true,
			wantPatches: 1,
			wantOut:     "saved_to_draft",
			wantPatch:   `{"revision":4,"updated_steps":{"screen":{"paywall":{"default_locale":"en_US","components_config":{"new":true},"components_localizations":{"en_US":{}}}}}}`,
		},
		{
			name:        "every edited step sends only its paywall",
			args:        []string{"wf_1", "--step-id", "screen"},
			stream:      streamStarted + streamEditedFull,
			saveStatus:  http.StatusOK,
			wantStream:  true,
			wantPatches: 1,
			wantOut:     "saved_to_draft",
			wantPatch: `{"revision":4,"updated_steps":{` +
				`"screen":{"paywall":{"default_locale":"en_US","components_config":{"a":1},"components_localizations":{},"state_declarations":{"x":1},"automatically_scale_font_size":false}},` +
				`"other":{"paywall":{"default_locale":"fr_FR","components_config":{"b":2},"components_localizations":{}}}}}`,
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
			var patchBody []byte
			var patches int
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
			if patches != tt.wantPatches {
				t.Fatalf("patches = %d, want %d", patches, tt.wantPatches)
			}
			if tt.wantOut != "" && !strings.Contains(out, tt.wantOut) {
				t.Fatalf("output missing %q: %s", tt.wantOut, out)
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
