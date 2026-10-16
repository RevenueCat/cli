package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTargetingReorderNeedsApprovalAndSendsPosition(t *testing.T) {
	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			if r.URL.Path != "/projects/proj/targeting_rules/trle1" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Error(err)
			}
		}
		_, _ = io.WriteString(w, `{"id":"trle1","rule_type":"legacy","state":"active","display_name":"US paywall","offering_id":"ofrng_us"}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	configPath := filepath.Join(t.TempDir(), "reorder.json")
	if err := os.WriteFile(configPath, []byte(`{"position":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"targeting", "update", "trle1", "--config", configPath, "--project-id", "proj", "--api-key", "sk_test", "--no-input"}
	_, _, err := runAgentCmd(t, args...)
	if err == nil || !strings.Contains(err.Error(), "--yes") || posted != nil {
		t.Fatalf("reorder should require approval: err=%v posted=%v", err, posted)
	}
	_, _, err = runAgentCmd(t, append(args, "--yes", "--json")...)
	if err != nil || posted["position"] != float64(1) {
		t.Fatalf("approved reorder failed: err=%v posted=%v", err, posted)
	}
}

func TestTargetingDeleteRequiresForceForLiveRule(t *testing.T) {
	for _, state := range []string{"active", "scheduled"} {
		t.Run(state, func(t *testing.T) {
			deletes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/projects/proj/targeting_rules/trle1" {
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
				if r.Method == http.MethodDelete {
					deletes++
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"trle1","rule_type":"legacy","state":"`+state+`","display_name":"US paywall","offering_id":"ofrng_us"}`)
			}))
			t.Cleanup(srv.Close)
			t.Setenv("RC_BASE_URL", srv.URL)
			args := []string{"targeting", "delete", "trle1", "--project-id", "proj", "--api-key", "sk_test", "--no-input"}
			_, _, err := runAgentCmd(t, append(args, "--yes")...)
			if err == nil || !strings.Contains(err.Error(), "--force") || deletes != 0 {
				t.Fatalf("expected force guard: err=%v deletes=%d", err, deletes)
			}
			_, _, err = runAgentCmd(t, append(args, "--force")...)
			if err == nil || !strings.Contains(err.Error(), "--yes") || deletes != 0 {
				t.Fatalf("expected confirmation after force: err=%v deletes=%d", err, deletes)
			}
			_, _, err = runAgentCmd(t, append(args, "--force", "--yes")...)
			if err != nil || deletes != 1 {
				t.Fatalf("approved force delete failed: err=%v deletes=%d", err, deletes)
			}
		})
	}
}

func TestTargetingDeleteInactiveNeedsOnlyConfirmation(t *testing.T) {
	deletes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"trle1","rule_type":"legacy","state":"inactive","display_name":"US paywall"}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	_, _, err := runAgentCmd(t, "targeting", "delete", "trle1", "--project-id", "proj", "--api-key", "sk_test", "--yes", "--no-input")
	if err != nil || deletes != 1 {
		t.Fatalf("inactive delete failed: err=%v deletes=%d", err, deletes)
	}
}

func TestTargetingActivationRequiresApproval(t *testing.T) {
	mutations := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			mutations++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"targeting_rule","id":"trle1","rule_type":"legacy","state":"active","display_name":"US paywall","offering_id":"ofrng_us"}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	configPath := filepath.Join(t.TempDir(), "rule.json")
	if err := os.WriteFile(configPath, []byte(`{"display_name":"US paywall","offering_id":"ofrng_us","state":"active"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"targeting", "create", "--config", configPath, "--project-id", "proj", "--api-key", "sk_test", "--no-input"}
	_, _, err := runAgentCmd(t, args...)
	if err == nil || !strings.Contains(err.Error(), "--yes") || mutations != 0 {
		t.Fatalf("expected approval error before mutation: err=%v mutations=%d", err, mutations)
	}
	args = append(args, "--yes", "--json")
	out, stderr, err := runAgentCmd(t, args...)
	if err != nil || stderr != "" || mutations != 1 || !strings.Contains(out, `"state": "active"`) {
		t.Fatalf("approved create failed: err=%v stderr=%q mutations=%d out=%s", err, stderr, mutations, out)
	}
}

func TestTargetingCreateScheduledOfferingRule(t *testing.T) {
	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Error(err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"targeting_rule","id":"trle1","rule_type":"legacy","state":"active","display_name":"Holiday paywall","offering_id":"ofrng_holiday","schedule":{"start_date":"2030-12-01T00:00:00Z"}}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	configPath := filepath.Join(t.TempDir(), "scheduled.json")
	if err := os.WriteFile(configPath, []byte(`{"display_name":"Holiday paywall","offering_id":"ofrng_holiday","state":"active","schedule":{"start_date":"2030-12-01T00:00:00Z"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"targeting", "create", "--config", configPath, "--project-id", "proj", "--api-key", "sk_test", "--no-input"}
	_, stderr, err := runAgentCmd(t, args...)
	if err == nil || !strings.Contains(err.Error(), "--yes") || posted != nil || !strings.Contains(stderr, "scheduled window") {
		t.Fatalf("scheduled create should show its plan and require approval: err=%v posted=%v stderr=%s", err, posted, stderr)
	}
	_, _, err = runAgentCmd(t, append(args, "--yes", "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	schedule, ok := posted["schedule"].(map[string]any)
	if !ok || posted["state"] != "active" || schedule["start_date"] != "2030-12-01T00:00:00Z" {
		t.Fatalf("scheduled Offering rule was not sent to the API: %v", posted)
	}
}

func TestTargetingActivationPreviewUsesReadableConditions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		state := "inactive"
		if r.Method == http.MethodPost {
			state = "active"
		}
		_, _ = io.WriteString(w, `{"object":"targeting_rule","id":"trle1","rule_type":"legacy","state":"`+state+`","display_name":"US paywall","offering_id":"ofrng_us","conditions":[{"context":null,"field":"country","operator":"in","value":["US"]}]}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	configPath := filepath.Join(t.TempDir(), "activate.json")
	if err := os.WriteFile(configPath, []byte(`{"state":"active"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := runAgentCmd(t, "targeting", "update", "trle1", "--config", configPath, "--project-id", "proj", "--api-key", "sk_test", "--yes", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(stderr, "country in US") < 2 || !strings.Contains(stderr, "Change state") || !strings.Contains(stderr, "active") || strings.Contains(stderr, `{"`) || strings.Contains(stderr, `[{`) {
		t.Fatalf("activation preview should be readable: %s", stderr)
	}
}

func TestTargetingUpdateActiveRuleRequiresApproval(t *testing.T) {
	mutations := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			mutations++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"targeting_rule","id":"trle1","rule_type":"legacy","state":"active","display_name":"US paywall","offering_id":"ofrng_us"}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	configPath := filepath.Join(t.TempDir(), "change.json")
	if err := os.WriteFile(configPath, []byte(`{"display_name":"US annual paywall"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := runAgentCmd(t, "targeting", "update", "trle1", "--config", configPath, "--project-id", "proj", "--api-key", "sk_test", "--no-input")
	if err == nil || !strings.Contains(err.Error(), "--yes") || mutations != 0 {
		t.Fatalf("expected approval error before mutation: err=%v mutations=%d", err, mutations)
	}
}

func TestTargetingUpdateWarnsWhenAudienceIsCleared(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"targeting_rule","id":"trle1","rule_type":"legacy","state":"active","display_name":"US paywall","offering_id":"ofrng_us","audience_id":"aud_us","conditions":[]}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	configPath := filepath.Join(t.TempDir(), "change.json")
	if err := os.WriteFile(configPath, []byte(`{"audience_id":null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := runAgentCmd(t, "targeting", "update", "trle1", "--config", configPath, "--project-id", "proj", "--api-key", "sk_test", "--no-input")
	if err == nil || !strings.Contains(err.Error(), "--yes") || !strings.Contains(stderr, "Resulting audience") || !strings.Contains(stderr, "matches everyone") {
		t.Fatalf("expected global-scope warning before approval; err=%v stderr=%q", err, stderr)
	}
}

func TestTargetingShowCheckpointDetailsAndJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"targeting_rule","id":"trle1","rule_type":"checkpoint","state":"scheduled","display_name":"After onboarding","flow_id":"wf1","audience_id":"aud1","checkpoints":[{"checkpoint_id":"chkpt1","position":2,"checkpoint":{"identifier":"app_open"}}],"schedule":{"start_date":null,"end_date":"2026-10-01T00:00:00Z"}}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	args := []string{"targeting", "show", "trle1", "--project-id", "proj", "--api-key", "sk_test", "--no-input"}
	out, _, err := runAgentCmd(t, args...)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"wf1", "aud1", "app_open (chkpt1)", "Position 2", "2026-10-01T00:00:00Z"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q from checkpoint view: %s", want, out)
		}
	}
	out, _, err = runAgentCmd(t, append(args, "--json")...)
	if err != nil || !strings.Contains(out, `"checkpoint_id": "chkpt1"`) || !strings.Contains(out, `"end_date": "2026-10-01T00:00:00Z"`) {
		t.Fatalf("JSON lost checkpoint details: err=%v out=%s", err, out)
	}
}

func TestTargetingListPreservesEvaluationOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != "active" {
			t.Errorf("state=%s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[{"id":"trle_z","display_name":"First match","rule_type":"legacy","state":"active","offering_id":"ofrng_z"},{"id":"trle_a","display_name":"Second match","rule_type":"legacy","state":"active","offering_id":"ofrng_a"}],"next_page":"/projects/proj/targeting_rules?state=active&starting_after=trle_a"}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	args := []string{"targeting", "list", "--state", "active", "--project-id", "proj", "--api-key", "sk_test", "--no-input"}
	out, stderr, err := runAgentCmd(t, args...)
	first, second := strings.Index(out, "trle_z"), strings.Index(out, "trle_a")
	if err != nil || first < 0 || second < 0 || first > second || !strings.Contains(stderr, "evaluation order") || !strings.Contains(stderr, "--cursor trle_a") {
		t.Fatalf("order or pagination missing: err=%v stdout=%s stderr=%s", err, out, stderr)
	}
	out, stderr, err = runAgentCmd(t, append(args, "--json")...)
	var envelope struct {
		Data struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
			NextPage string `json:"next_page"`
		} `json:"data"`
	}
	if err != nil || stderr != "" {
		t.Fatalf("JSON list failed: err=%v stderr=%s", err, stderr)
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Items) != 2 || envelope.Data.Items[0].ID != "trle_z" || envelope.Data.Items[1].ID != "trle_a" || envelope.Data.NextPage == "" {
		t.Fatalf("JSON lost order or pagination: %s", out)
	}
}

func TestTargetingUpdateScheduleRequiresApprovalAndPreservesBounds(t *testing.T) {
	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Error(err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"trle1","rule_type":"legacy","state":"inactive","display_name":"Holiday paywall","offering_id":"ofrng_holiday"}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("RC_BASE_URL", srv.URL)
	configPath := filepath.Join(t.TempDir(), "schedule.json")
	if err := os.WriteFile(configPath, []byte(`{"state":"active","schedule":{"start_date":"2030-12-01T00:00:00Z","end_date":"2030-12-31T23:59:59Z"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"targeting", "update", "trle1", "--config", configPath, "--project-id", "proj", "--api-key", "sk_test", "--no-input"}
	_, stderr, err := runAgentCmd(t, args...)
	if err == nil || !strings.Contains(err.Error(), "--yes") || posted != nil || !strings.Contains(stderr, "2030-12-01T00:00:00Z") || !strings.Contains(stderr, "2030-12-31T23:59:59Z") {
		t.Fatalf("schedule was not reviewed before mutation: err=%v posted=%v stderr=%s", err, posted, stderr)
	}
	_, _, err = runAgentCmd(t, append(args, "--yes", "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	schedule, ok := posted["schedule"].(map[string]any)
	if !ok || posted["state"] != "active" || schedule["start_date"] != "2030-12-01T00:00:00Z" || schedule["end_date"] != "2030-12-31T23:59:59Z" {
		t.Fatalf("schedule bounds missing from request: %v", posted)
	}
}
