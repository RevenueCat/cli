package cli_test

// Output snapshot tests: lock the human-mode layout and copy of
// representative commands into golden files so design regressions fail CI
// instead of being discovered by a human squinting at a terminal.
//
// Regenerate after intentional design changes:
//
//	UPDATE_SNAPSHOTS=1 go test ./internal/cli/ -run TestOutputSnapshots
//
// Then eyeball `make preview` (renders the goldens to SVGs) and commit both.
// Goldens are colorless (tests run without a TTY); color choices live in
// internal/output/brand.go and are reviewed there.

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func snapshotServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/offerings/ofrng_snap"):
			io.WriteString(w, `{"object":"offering","id":"ofrng_snap","lookup_key":"default","display_name":"Default","is_current":true,"created_at":1784297950368,"project_id":"proj_snap"}`)
		case strings.HasSuffix(r.URL.Path, "/apps/app_snap1"):
			io.WriteString(w, `{"object":"app","id":"app_snap1","name":"Moodly (App Store)","type":"app_store","created_at":1784241909459,"project_id":"proj_snap","app_store":{"bundle_id":"com.example.moodly","subscription_key_configured":true,"app_store_connect_api_key_configured":false}}`)
		case strings.HasSuffix(r.URL.Path, "/projects"):
			io.WriteString(w, `{"object":"list","items":[{"id":"proj_snap","name":"Moodly"},{"id":"proj_snap2","name":"Moodly Staging"}],"next_page":null}`)
		case strings.HasSuffix(r.URL.Path, "/proj_snap2/apps"):
			io.WriteString(w, `{"object":"list","items":[{"object":"app","id":"app_snap3","name":"Moodly Staging (App Store)","type":"app_store","created_at":1784241909459,"project_id":"proj_snap2","app_store":{"bundle_id":"com.example.moodly","subscription_key_configured":true,"app_store_connect_api_key_configured":true}}],"next_page":null,"url":"/apps"}`)
		case strings.HasSuffix(r.URL.Path, "/apps"):
			io.WriteString(w, `{"object":"list","items":[{"object":"app","id":"app_snap1","name":"Moodly (App Store)","type":"app_store","created_at":1784241909459,"project_id":"proj_snap","app_store":{"bundle_id":"com.example.moodly","subscription_key_configured":true,"app_store_connect_api_key_configured":false}},{"object":"app","id":"app_snap2","name":"Test Store","type":"test_store","created_at":1784241905823,"project_id":"proj_snap"}],"next_page":null,"url":"/apps"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"object":"error","type":"resource_missing","message":"not found"}`)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestOutputSnapshots(t *testing.T) {
	server := snapshotServer(t)
	t.Setenv("RC_BASE_URL", server.URL)

	scenarios := []struct {
		name string
		args []string
	}{
		{"version", []string{"version"}},
		{"auth-status-logged-out", []string{"auth", "status", "--no-input"}},
		{"offerings-show", []string{"offerings", "show", "ofrng_snap", "--no-input", "--project-id", "proj_snap", "--api-key", "sk_snap"}},
		{"apps-list", []string{"apps", "list", "--no-input", "--project-id", "proj_snap", "--api-key", "sk_snap"}},
		{"apps-list-all-projects", []string{"apps", "list", "--all-projects", "--bundle-id", "com.example.moodly", "--no-input", "--api-key", "sk_snap"}},
		{"error-not-found", []string{"offerings", "show", "ofrng_missing", "--no-input", "--project-id", "proj_snap", "--api-key", "sk_snap"}},
		{"apps-apple-setup", []string{"apps", "apple", "setup", "app_snap1", "--no-input", "--project-id", "proj_snap", "--api-key", "sk_snap"}},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			stdout, stderr, err := runAgentCmd(t, sc.args...)
			var b strings.Builder
			fmt.Fprintf(&b, "$ rc %s\n", strings.Join(sc.args, " "))
			if stderr != "" {
				b.WriteString(stderr)
			}
			if stdout != "" {
				b.WriteString(stdout)
			}
			if err != nil {
				fmt.Fprintf(&b, "(exit: %v)\n", err)
			}
			got := b.String()

			golden := filepath.Join("testdata", "snapshots", sc.name+".golden")
			if os.Getenv("UPDATE_SNAPSHOTS") != "" {
				if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("missing golden %s — run UPDATE_SNAPSHOTS=1 go test ./internal/cli/ -run TestOutputSnapshots", golden)
			}
			if string(want) != got {
				t.Errorf("output changed vs %s\n--- want\n%s\n--- got\n%s\nIf intentional: UPDATE_SNAPSHOTS=1 go test ./internal/cli/ -run TestOutputSnapshots, review `make preview`, commit both.", golden, want, got)
			}
		})
	}
}
