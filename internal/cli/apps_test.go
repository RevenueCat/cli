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

// Locating an app by store identifier must work org-wide without an active
// project: --all-projects walks every project, --bundle-id narrows to the app.
func TestAppsList_AllProjectsFindsBundleIDAcrossProjects(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", configDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/projects":
			_, _ = io.WriteString(w, `{"object":"list","items":[{"id":"proj_a","name":"Alpha"},{"id":"proj_b","name":"Beta"}],"next_page":null}`)
		case "/projects/proj_a/apps":
			_, _ = io.WriteString(w, `{"object":"list","items":[{"object":"app","id":"app_a1","name":"Alpha iOS","type":"app_store","project_id":"proj_a","app_store":{"bundle_id":"com.alpha.app"}}],"next_page":null}`)
		case "/projects/proj_b/apps":
			_, _ = io.WriteString(w, `{"object":"list","items":[{"object":"app","id":"app_b1","name":"Beta iOS","type":"app_store","project_id":"proj_b","app_store":{"bundle_id":"com.beta.app"}},{"object":"app","id":"app_b2","name":"Beta Android","type":"play_store","project_id":"proj_b","play_store":{"package_name":"com.beta.app"}}],"next_page":null}`)
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	// No ProjectID on purpose: --all-projects must not require one.
	if err := config.Save("", &config.Config{APIKey: "sk_test", BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}

	out, _, err := runCmdInConfigDir(t, configDir,
		"apps", "list", "--all-projects", "--bundle-id", "com.beta.app", "--json", "--no-input")
	if err != nil {
		t.Fatalf("apps list --all-projects failed: %v", err)
	}
	var env struct {
		Data struct {
			Items []struct {
				ID        string `json:"id"`
				ProjectID string `json:"project_id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output not JSON: %v\n%s", err, out)
	}
	if len(env.Data.Items) != 1 {
		t.Fatalf("want exactly the bundle-id match, got %+v", env.Data.Items)
	}
	if env.Data.Items[0].ID != "app_b1" || env.Data.Items[0].ProjectID != "proj_b" {
		t.Errorf("want app_b1 in proj_b, got %+v", env.Data.Items[0])
	}

	// --package-name matches the Play Store app, not the same-named bundle ID.
	out, _, err = runCmdInConfigDir(t, configDir,
		"apps", "list", "--all-projects", "--package-name", "com.beta.app", "--json", "--no-input")
	if err != nil {
		t.Fatalf("apps list --package-name failed: %v", err)
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output not JSON: %v\n%s", err, out)
	}
	if len(env.Data.Items) != 1 || env.Data.Items[0].ID != "app_b2" {
		t.Errorf("want app_b2 for the package-name match, got %+v", env.Data.Items)
	}
}

// Without --all-projects the filters still apply, scoped to the active project.
func TestAppsList_BundleIDFilterWithinProject(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", configDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/projects/proj_x/apps" {
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"object":"list","items":[{"object":"app","id":"app_1","name":"iOS","type":"app_store","project_id":"proj_x","app_store":{"bundle_id":"com.acme.app"}},{"object":"app","id":"app_2","name":"Android","type":"play_store","project_id":"proj_x","play_store":{"package_name":"com.acme.app"}}],"next_page":null}`)
	}))
	t.Cleanup(server.Close)
	if err := config.Save("", &config.Config{APIKey: "sk_test", ProjectID: "proj_x", BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}

	// Bundle IDs compare case-insensitively.
	out, _, err := runCmdInConfigDir(t, configDir,
		"apps", "list", "--bundle-id", "COM.ACME.APP", "--json", "--no-input")
	if err != nil {
		t.Fatalf("apps list --bundle-id failed: %v", err)
	}
	var env struct {
		Data struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output not JSON: %v\n%s", err, out)
	}
	if len(env.Data.Items) != 1 || env.Data.Items[0].ID != "app_1" {
		t.Errorf("want only the App Store app, got %+v", env.Data.Items)
	}
}

// Regression: under --json (non-interactive) the command must error on missing
// required input, not attempt a prompt and then fail confusingly.
func TestAppsCreate_JSONErrorsOnMissingInputInsteadOfPrompting(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", configDir)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request should be made when required input is missing: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	if err := config.Save("", &config.Config{APIKey: "sk_test", ProjectID: "proj_x", BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}

	_, _, err := runCmdInConfigDir(t, configDir, "apps", "create", "--json")
	if err == nil || !strings.Contains(err.Error(), "--name") {
		t.Fatalf("want a clean --name error under --json, got: %v", err)
	}

	_, _, err = runCmdInConfigDir(t, configDir, "apps", "create", "--json", "--name", "Acme", "--type", "app_store")
	if err == nil || !strings.Contains(err.Error(), "--bundle-id") {
		t.Fatalf("want a clean --bundle-id error under --json, got: %v", err)
	}
}
