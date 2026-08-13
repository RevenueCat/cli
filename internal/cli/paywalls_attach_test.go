package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPaywallsAttachAndDetachSendCurrentRevision(t *testing.T) {
	tests := []struct {
		name       string
		command    string
		args       []string
		offeringID any
	}{
		{
			name:       "attach",
			command:    "attach",
			args:       []string{"pw_test", "ofrng_test"},
			offeringID: "ofrng_test",
		},
		{
			name:       "detach",
			command:    "detach",
			args:       []string{"pw_test"},
			offeringID: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var patchBody map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.Method {
				case http.MethodGet:
					if r.URL.Query().Get("expand") != "components" {
						t.Errorf("expand = %q, want components", r.URL.Query().Get("expand"))
					}
					_, _ = io.WriteString(w, `{"id":"pw_test","offering_id":null,"created_at":1,"published_at":null,"components":{"published":null,"draft":{"revision":7,"components_config":{},"components_localizations":{},"default_locale":"en_US"}}}`)
				case http.MethodPatch:
					if err := json.NewDecoder(r.Body).Decode(&patchBody); err != nil {
						t.Fatal(err)
					}
					_, _ = io.WriteString(w, `{"id":"pw_test","offering_id":null,"created_at":1,"published_at":null}`)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()

			t.Setenv("RC_CONFIG_DIR", t.TempDir())
			t.Setenv("RC_BASE_URL", server.URL)
			root := NewRootCmd("test")
			root.SetOut(io.Discard)
			root.SetErr(&bytes.Buffer{})
			args := append([]string{"paywalls", tt.command}, tt.args...)
			args = append(args, "--no-input", "--api-key", "sk_test", "--project-id", "proj")
			root.SetArgs(args)
			if err := root.ExecuteContext(context.Background()); err != nil {
				t.Fatal(err)
			}

			if patchBody["revision"] != float64(7) {
				t.Fatalf("revision = %v, want 7", patchBody["revision"])
			}
			if patchBody["offering_id"] != tt.offeringID {
				t.Fatalf("offering_id = %v, want %v", patchBody["offering_id"], tt.offeringID)
			}
		})
	}
}
