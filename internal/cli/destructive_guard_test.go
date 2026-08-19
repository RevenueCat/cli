package cli_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/config"
)

// Every command that removes or mutates real state must refuse to act under
// --no-input unless --yes is passed, and --yes must let it through. This drives
// the real commands; a new destructive command is covered by adding it here.
//
// The fake server allows read-only GET preflight (some commands fetch state to
// decide whether extra confirmation is needed) but fails the test on any write,
// proving nothing is destroyed before consent.
func TestDestructiveCommands_RefuseUnderNoInputWithoutYes(t *testing.T) {
	// The most destructive, irreversible commands are interactive-only and are
	// covered separately below; --yes bypasses confirmation for everything here.
	commands := [][]string{
		{"products", "push", "prod_x"},
		{"paywalls", "publish", "pw_x"},
		{"paywalls", "unpublish", "pw_x"},
		{"webhooks", "delete", "wh_x"},
		{"purchases", "refund", "txn_x"},
		{"subscriptions", "cancel", "sub_x"},
		{"subscriptions", "refund", "sub_x"},
		{"products", "store", "discard", "plan_x"},
		{"customer", "revoke", "cust_x", "ent_x"},
		{"customer", "grant", "cust_x", "ent_x", "--duration", "monthly"},
		{"customer", "transfer", "cust_x", "--to", "cust_y"},
	}
	for _, args := range commands {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			configDir := t.TempDir()
			t.Setenv("RC_CONFIG_DIR", configDir)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("%s %s: state-changing call reached the network before confirmation", r.Method, r.URL.Path)
					http.Error(w, "unexpected write before confirmation", http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			t.Cleanup(server.Close)
			if err := config.Save("", &config.Config{APIKey: "sk_test", ProjectID: "proj_test", BaseURL: server.URL}); err != nil {
				t.Fatal(err)
			}

			runArgs := append(append([]string{}, args...), "--no-input")
			_, _, err := runCmdInConfigDir(t, configDir, runArgs...)
			if err == nil {
				t.Fatal("want refusal under --no-input without --yes")
			}
			if !strings.Contains(err.Error(), "pass --yes") {
				t.Fatalf("want the confirmation gate error, got: %v", err)
			}
		})
	}
}

func TestDestructiveCommand_YesBypassesConfirmation(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", configDir)
	var deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/webhooks/wh_x"):
			deleted = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	if err := config.Save("", &config.Config{APIKey: "sk_test", ProjectID: "proj_test", BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}

	_, errb, err := runCmdInConfigDir(t, configDir, "webhooks", "delete", "wh_x", "--yes", "--no-input")
	if err != nil {
		t.Fatalf("--yes should let the delete proceed: %v\nstderr: %s", err, errb)
	}
	if !deleted {
		t.Fatal("--yes did not let the command reach the delete call")
	}
}

// The irreversible, customer-facing deletes are interactive-only: --yes does NOT
// buy a way through, and --json/--no-input are refused outright so automation
// can't fire them. A new human-only command is covered by adding it here.
func TestInteractiveOnlyCommands_RefuseEvenWithYes(t *testing.T) {
	commands := [][]string{
		{"apps", "delete", "app_x"},
		{"paywalls", "delete", "pw_x"},
		{"offerings", "delete", "ofrng_x"},
		{"entitlements", "delete", "ent_x"},
		{"products", "delete", "prod_x"},
		{"packages", "delete", "pkg_x"},
	}
	for _, args := range commands {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			configDir := t.TempDir()
			t.Setenv("RC_CONFIG_DIR", configDir)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("%s %s: interactive-only command reached the network", r.Method, r.URL.Path)
				http.Error(w, "must not touch the network", http.StatusInternalServerError)
			}))
			t.Cleanup(server.Close)
			if err := config.Save("", &config.Config{APIKey: "sk_test", ProjectID: "proj_test", BaseURL: server.URL}); err != nil {
				t.Fatal(err)
			}

			// --yes present on purpose: it must NOT bypass the interactive gate.
			runArgs := append(append([]string{}, args...), "--yes", "--no-input")
			_, _, err := runCmdInConfigDir(t, configDir, runArgs...)
			if err == nil {
				t.Fatal("want refusal: interactive-only even with --yes")
			}
			if !strings.Contains(err.Error(), "interactive-only") {
				t.Fatalf("want the interactive-only gate error, got: %v", err)
			}
		})
	}
}
