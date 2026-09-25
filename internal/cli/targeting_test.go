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
