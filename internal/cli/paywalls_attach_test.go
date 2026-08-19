package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestPaywallsAttachAndDetachHintOnAPIError(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		patchStatus int
		patchBody   string
		wantErr     string
		wantHint    string
	}{
		{
			name:        "attach 409 offering already has a paywall",
			args:        []string{"attach", "pw_test", "ofrng_test"},
			patchStatus: http.StatusConflict,
			patchBody:   `{"type":"resource_already_exists","message":"There is already a paywall for offering ofrng_test"}`,
			wantErr:     "offering ofrng_test already has a paywall",
			wantHint:    "rc paywalls detach",
		},
		{
			name:        "detach 422 published paywall",
			args:        []string{"detach", "pw_test"},
			patchStatus: http.StatusUnprocessableEntity,
			patchBody:   `{"type":"parameter_error","message":"offering_id cannot be removed from a published paywall"}`,
			wantErr:     "detaching offering from paywall pw_test",
			wantHint:    "rc paywalls unpublish pw_test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.Method {
				case http.MethodGet:
					_, _ = io.WriteString(w, `{"id":"pw_test","offering_id":null,"created_at":1,"published_at":null,"components":{"published":null,"draft":{"revision":7}}}`)
				case http.MethodPatch:
					w.WriteHeader(tt.patchStatus)
					_, _ = io.WriteString(w, tt.patchBody)
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
			root.SetArgs(append([]string{"paywalls"}, append(tt.args, "--no-input", "--api-key", "sk_test", "--project-id", "proj")...))
			err := root.ExecuteContext(context.Background())
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err, tt.wantErr)
			}
			if hint := hintFor(err); !strings.Contains(hint, tt.wantHint) {
				t.Fatalf("hint = %q, want it to contain %q", hint, tt.wantHint)
			}
		})
	}
}

func TestPaywallsAttachPublishedRequiresConfirmation(t *testing.T) {
	var patches int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			_, _ = io.WriteString(w, `{"id":"pw_test","offering_id":null,"created_at":1,"published_at":1700000000000,"components":{"published":null,"draft":{"revision":7}}}`)
		case http.MethodPatch:
			patches++
			_, _ = io.WriteString(w, `{"id":"pw_test","offering_id":"ofrng_test","created_at":1,"published_at":1700000000000}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Setenv("RC_CONFIG_DIR", t.TempDir())
	t.Setenv("RC_BASE_URL", server.URL)

	attach := func(extra ...string) error {
		root := NewRootCmd("test")
		root.SetOut(io.Discard)
		root.SetErr(&bytes.Buffer{})
		args := []string{"paywalls", "attach", "pw_test", "ofrng_test", "--no-input", "--api-key", "sk_test", "--project-id", "proj"}
		root.SetArgs(append(args, extra...))
		return root.ExecuteContext(context.Background())
	}

	if err := attach(); err == nil {
		t.Fatal("attaching a published paywall under --no-input without --yes should fail")
	}
	if patches != 0 {
		t.Fatalf("refusal must not issue PATCH, got %d", patches)
	}

	if err := attach("--yes"); err != nil {
		t.Fatalf("--yes should attach published paywall: %v", err)
	}
	if patches != 1 {
		t.Fatalf("PATCH count = %d, want 1", patches)
	}
}
