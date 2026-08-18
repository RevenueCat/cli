package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/config"
)

func TestAppsCreate_NoInputWithoutTypeErrorsBeforeCreating(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", configDir)

	var createBodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/projects/proj_x/apps" {
			b, _ := io.ReadAll(r.Body)
			createBodies = append(createBodies, string(b))
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"app_new","name":"Acme","type":"play_store"}`)
			return
		}
		http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	if err := config.Save("", &config.Config{APIKey: "sk_test", ProjectID: "proj_x", BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}

	_, _, err := runCmdInConfigDir(t, configDir, "apps", "create", "--name", "Acme", "--no-input")
	if err == nil {
		t.Fatal("want error when --type omitted under --no-input")
	}
	if !strings.Contains(err.Error(), "--type") {
		t.Fatalf("error should name --type, got: %v", err)
	}
	if len(createBodies) != 0 {
		t.Fatalf("no create request should be made when --type is missing, got: %v", createBodies)
	}

	out, _, err := runCmdInConfigDir(t, configDir,
		"apps", "create", "--name", "Acme", "--type", "play_store", "--package-name", "com.acme.app", "--no-input", "--json")
	if err != nil {
		t.Fatalf("create with --type should succeed: %v", err)
	}
	if len(createBodies) != 1 {
		t.Fatalf("want exactly one create request, got %d", len(createBodies))
	}
	var sent struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(createBodies[0]), &sent); err != nil {
		t.Fatalf("create body not JSON: %v", err)
	}
	if sent.Type != "play_store" {
		t.Fatalf("want type play_store from flag, got %q", sent.Type)
	}
	if !strings.Contains(out, "app_new") {
		t.Fatalf("expected created app in output: %s", out)
	}
}
