package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func runScreens(t *testing.T, serverURL string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	t.Setenv("RC_BASE_URL", serverURL)
	root := NewRootCmd("test")
	var out, errb bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(append([]string{"paywalls", "screens"}, append(args, "--api-key", "sk_test", "--project-id", "proj")...))
	err = root.ExecuteContext(context.Background())
	return out.String(), errb.String(), err
}

// The purchase screen must come from the graph's paywall_step_id, never from
// a step's is_terminal — step_1 here is terminal but step_2 is the fallback.
func TestPaywallsScreens_MarksPurchaseScreenFromPaywallStepID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/proj/paywalls/pw_abc/graph" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"object":"paywall_graph","id":"pw_abc","version":"draft","graph":{
			"revision":3,"initial_step_id":"step_1","paywall_step_id":"step_2","total_steps":2,
			"steps":[
				{"id":"step_1","name":"Intro","type":"screen","screen_types":["generic"],"is_terminal":true,"paywall_id":"pw_intro","edges":[],"unwired_triggers":[]},
				{"id":"step_2","name":"Purchase","type":"screen","screen_types":["paywall"],"is_terminal":false,"paywall_id":"pw_abc","edges":[],"unwired_triggers":[]}
			]}}`)
	}))
	defer server.Close()

	stdout, _, err := runScreens(t, server.URL, "pw_abc")
	if err != nil {
		t.Fatalf("err = %v, stdout = %s", err, stdout)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 3 { // header + 2 rows
		t.Fatalf("expected a header and 2 rows, got:\n%s", stdout)
	}
	if !strings.Contains(lines[1], "step_1") || strings.Contains(lines[1], " yes") {
		t.Fatalf("step_1 (terminal, not the fallback) must not be marked purchase: %s", lines[1])
	}
	if !strings.Contains(lines[2], "step_2") || !strings.Contains(lines[2], "yes") {
		t.Fatalf("step_2 (the graph's paywall_step_id) must be marked purchase: %s", lines[2])
	}
}

// A standalone V2 paywall reports graph: null; screens must say so plainly
// rather than crashing or fabricating a row.
func TestPaywallsScreens_StandaloneHasNoGraph(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"object":"paywall_graph","id":"pw_solo","version":"draft","graph":null}`)
	}))
	defer server.Close()

	stdout, stderr, err := runScreens(t, server.URL, "pw_solo")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(stdout+stderr, "standalone") {
		t.Fatalf("expected a standalone explanation, got stdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}
