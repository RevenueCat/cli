package cli_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
