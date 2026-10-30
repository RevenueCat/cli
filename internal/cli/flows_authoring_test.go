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

	"github.com/revenuecat/cli/internal/cli"
)

func TestFlowExportUpdateRoundTrip(t *testing.T) {
	var update map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/proj/flows/wf_1/draft" {
			t.Errorf("path = %s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = io.WriteString(w, `{"id":"wf_1","object":"flow_draft","revision":4,"type":"web-funnel","display_name":"Checkout","initial_step_id":"screen","single_step_fallback_id":null,"app_id":null,"metadata":{"large":9007199254740993},"custom_context_parameters_config":null,"content_max_width":null,"steps":{"screen":{"type":"screen","paywall":{"components_config":{"large":9007199254740993},"components_localizations":{"en_US":{"title":"Hello"}},"haptics_enabled":false}}}}`)
		case http.MethodPatch:
			if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
				t.Error(err)
			}
			_, _ = io.WriteString(w, `{"id":"wf_1","revision":5}`)
		default:
			t.Errorf("method = %s", r.Method)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "flow.json")
	out, _, err := runProjectSetupCommand(t, server.URL, "flows", "export", "wf_1", "--output", path, "--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"schema_version"`) {
		t.Fatalf("missing envelope: %s", out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "schema_version") {
		t.Fatal("export contains CLI envelope")
	}
	_, _, err = runProjectSetupCommand(t, server.URL, "flows", "update", "wf_1", "--config", path, "--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if string(update["revision"]) != "4" || string(update["app_id"]) != "null" {
		t.Fatalf("revision/null changed: %s", update)
	}
	if !strings.Contains(string(update["updated_steps"]), "9007199254740993") || !strings.Contains(string(update["metadata"]), "9007199254740993") {
		t.Fatal("large integer lost precision")
	}
	if !strings.Contains(string(update["updated_steps"]), `"haptics_enabled":false`) || !strings.Contains(string(update["updated_steps"]), `"en_US"`) {
		t.Fatalf("screen fields lost: %s", update["updated_steps"])
	}
	if _, exists := update["deleted_step_ids"]; exists {
		t.Fatal("export inferred step deletions")
	}
}

func TestFlowValidationInvalidRendersIssuesAndFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/proj/flows/wf_1/actions/validate" || r.Method != "POST" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"object":"flow_validation","id":"wf_1","revision":4,"valid":false,"step_issues":{},"workflow_errors":[{"key":"no_initial_step","message":"Choose a start step","severity":"error"}]}`)
	}))
	defer server.Close()
	out, _, err := runProjectSetupCommand(t, server.URL, "flows", "validate", "wf_1", "--revision", "4", "--json", "--no-input")
	if cli.ExitCodeFor(err) != 1 {
		t.Fatalf("exit = %d, error = %v", cli.ExitCodeFor(err), err)
	}
	if !strings.Contains(out, "Choose a start step") || !strings.Contains(out, `"valid": false`) {
		t.Fatalf("validation output = %s", out)
	}
}

func TestFlowUpdateRejectsInvalidConfigsBeforeRequest(t *testing.T) {
	for _, data := range []string{`{"revision":1,"unknown":true}`, `{"revision":1} {"revision":2}`} {
		path := filepath.Join(t.TempDir(), "flow.json")
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		_, _, err := runProjectSetupCommand(t, "http://127.0.0.1:0", "flows", "update", "wf_1", "--config", path, "--no-input")
		if err == nil || strings.Contains(err.Error(), "connection refused") {
			t.Fatalf("config %s was not rejected before HTTP: %v", data, err)
		}
	}
}

func TestFlowCreateAndDuplicateRemainUnpublished(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Method != "POST" {
			t.Errorf("method = %s", r.Method)
		}
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path == "/projects/proj/flows" {
			if string(body["type"]) != `"web-funnel"` || !strings.Contains(string(body["steps"]), `"screen"`) {
				t.Errorf("create body = %s", body)
			}
		} else if r.URL.Path == "/projects/proj/flows/wf_1/actions/duplicate" {
			if string(body["source_version"]) != `"published"` || string(body["display_name"]) != `"Copy"` {
				t.Errorf("duplicate body = %s", body)
			}
		} else {
			t.Errorf("unexpected mutation: %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":"wf_copy","revision":1,"published_at":null}`)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "flow.json")
	if err := os.WriteFile(path, []byte(`{"display_name":"Original","type":"web-funnel","steps":{"screen":{"type":"screen"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"flows", "create", "--config", path, "--json", "--no-input"},
		{"flows", "duplicate", "wf_1", "--name", "Copy", "--source-version", "published", "--json", "--no-input"},
	} {
		if _, _, err := runProjectSetupCommand(t, server.URL, args...); err != nil {
			t.Fatal(err)
		}
	}
	if len(paths) != 2 {
		t.Fatalf("requests = %v", paths)
	}
}
