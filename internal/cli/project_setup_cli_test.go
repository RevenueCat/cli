package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/cli"
)

func TestOfferingsSetCurrent_RequiresConfirmationAndUpdatesOffering(t *testing.T) {
	var isCurrent *bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/projects/proj/offerings/ofrng" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		var body struct {
			IsCurrent *bool `json:"is_current"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		isCurrent = body.IsCurrent
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"ofrng","lookup_key":"default","display_name":"Default","is_current":true,"state":"active","object":"offering","created_at":1,"metadata":null}`)
	}))
	t.Cleanup(server.Close)

	out, _, err := runProjectSetupCommand(t, server.URL,
		"offerings", "set-current", "ofrng", "--yes", "--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if isCurrent == nil || !*isCurrent || !strings.Contains(out, `"is_current": true`) {
		t.Fatalf("is_current = %v, output = %s", isCurrent, out)
	}
}

func TestOfferingsSetCurrent_NoInputRequiresYes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request made without confirmation")
	}))
	t.Cleanup(server.Close)

	_, _, err := runProjectSetupCommand(t, server.URL,
		"offerings", "set-current", "ofrng", "--json", "--no-input")
	if err == nil || !strings.Contains(err.Error(), "pass --yes") {
		t.Fatalf("error = %v, want --yes guidance", err)
	}
}

func TestAppsKeys_ReturnsTypedPublicSDKKeys(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/projects/proj/apps/app/public_api_keys" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","items":[{"id":"key","object":"public_api_key","app_id":"app","environment":"production","key":"appl_test","created_at":1}],"next_page":null,"url":"/keys"}`)
	}))
	t.Cleanup(server.Close)

	out, _, err := runProjectSetupCommand(t, server.URL,
		"apps", "keys", "app", "--json", "--no-input")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"key": "appl_test"`) || !strings.Contains(out, `"environment": "production"`) {
		t.Fatalf("unexpected SDK key output: %s", out)
	}
}

func runProjectSetupCommand(t *testing.T, baseURL string, args ...string) (string, string, error) {
	t.Helper()
	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	t.Setenv("RC_BASE_URL", baseURL)
	var stdout, stderr bytes.Buffer
	root := cli.NewRootCmd("test")
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	args = append(args, "--api-key", "sk_test", "--project-id", "proj")
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return stdout.String(), stderr.String(), err
}
