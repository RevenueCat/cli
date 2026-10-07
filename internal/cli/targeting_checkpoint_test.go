package cli_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckpointTargetingUpdateGuardsLiveAndScheduledChanges(t *testing.T) {
	for _, tc := range []struct {
		name, currentState, newState string
		approval                     bool
	}{
		{"activate", "inactive", "active", true},
		{"schedule", "inactive", "scheduled", true},
		{"edit active", "active", "", true},
		{"edit scheduled", "scheduled", "", true},
		{"edit inactive", "inactive", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var posted map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/projects/proj/targeting_rules/chkptrule1" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Method == http.MethodPost {
					if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
						t.Error(err)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"id":"chkptrule1","rule_type":"checkpoint","state":%q,"display_name":"Onboarding","flow_id":"wf_original","audience_id":"aud_original"}`, tc.currentState)
			}))
			t.Cleanup(srv.Close)
			t.Setenv("RC_BASE_URL", srv.URL)
			config := map[string]any{
				"display_name": "New onboarding", "flow_id": "wf_new", "audience_id": "aud_new",
				"checkpoints": []map[string]string{{"checkpoint_id": "chkpt_new"}},
				"schedule":    map[string]string{"start_date": "2030-12-01T00:00:00Z", "end_date": "2030-12-31T23:59:59Z"},
			}
			if tc.newState != "" {
				config["state"] = tc.newState
			}
			data, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(t.TempDir(), "checkpoint.json")
			if err := os.WriteFile(configPath, data, 0o600); err != nil {
				t.Fatal(err)
			}
			args := []string{"targeting", "update", "chkptrule1", "--config", configPath, "--project-id", "proj", "--api-key", "sk_test", "--no-input"}
			if tc.approval {
				_, preview, err := runAgentCmd(t, args...)
				if err == nil || !strings.Contains(err.Error(), "--yes") || posted != nil {
					t.Fatalf("approval did not precede mutation: err=%v posted=%v", err, posted)
				}
				for _, want := range []string{"Current flow", "wf_original", "wf_new", "chkpt_new", "2030-12-01T00:00:00Z"} {
					if !strings.Contains(preview, want) {
						t.Fatalf("missing %q in preview: %s", want, preview)
					}
				}
				args = append(args, "--yes")
			}
			out, stderr, err := runAgentCmd(t, append(args, "--json")...)
			if err != nil || stderr != "" || posted == nil || !strings.Contains(out, `"rule_type": "checkpoint"`) {
				t.Fatalf("checkpoint update failed: err=%v posted=%v stdout=%s stderr=%s", err, posted, out, stderr)
			}
			postedJSON, err := json.Marshal(posted)
			if err != nil || string(postedJSON) != string(data) {
				t.Fatalf("request changed: got=%s want=%s err=%v", postedJSON, data, err)
			}
		})
	}
}

func TestTargetingUpdateRejectsFieldsForWrongRuleType(t *testing.T) {
	for _, tc := range []struct{ name, ruleType, config, want string }{
		{"checkpoint position", "checkpoint", `{"position":1}`, "cannot be set on checkpoint"},
		{"checkpoint offering", "checkpoint", `{"offering_id":"ofrng1"}`, "cannot be set on checkpoint"},
		{"checkpoint conditions", "checkpoint", `{"conditions":[]}`, "cannot be set on checkpoint"},
		{"checkpoint placements", "checkpoint", `{"placements":null}`, "cannot be set on checkpoint"},
		{"checkpoint null audience", "checkpoint", `{"audience_id":null}`, "nonempty string"},
		{"Offering flow", "legacy", `{"flow_id":"wf1"}`, "only be set on checkpoint"},
		{"Offering checkpoints", "legacy", `{"checkpoints":[{"checkpoint_id":"chkpt1"}]}`, "only be set on checkpoint"},
		{"Offering scheduled state", "legacy", `{"state":"scheduled"}`, "state must be active or inactive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mutations := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					mutations++
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"id":"rule1","rule_type":%q,"state":"inactive"}`, tc.ruleType)
			}))
			t.Cleanup(srv.Close)
			t.Setenv("RC_BASE_URL", srv.URL)
			configPath := filepath.Join(t.TempDir(), "update.json")
			if err := os.WriteFile(configPath, []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}
			_, _, err := runAgentCmd(t, "targeting", "update", "rule1", "--config", configPath, "--project-id", "proj", "--api-key", "sk_test", "--no-input", "--yes")
			if err == nil || !strings.Contains(err.Error(), tc.want) || mutations != 0 {
				t.Fatalf("err=%v mutations=%d; want %q before mutation", err, mutations, tc.want)
			}
		})
	}
}
