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
	commands := [][]string{
		{"apps", "delete", "app_x"},
		{"products", "delete", "prod_x"},
		{"products", "push", "prod_x"},
		{"paywalls", "delete", "pw_x"},
		{"paywalls", "publish", "pw_x"},
		{"paywalls", "unpublish", "pw_x"},
		{"offerings", "delete", "ofrng_x"},
		{"packages", "delete", "pkg_x"},
		{"entitlements", "delete", "ent_x"},
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
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/apps/app_x") {
			deleted = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		http.Error(w, "unexpected request", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	if err := config.Save("", &config.Config{APIKey: "sk_test", ProjectID: "proj_test", BaseURL: server.URL}); err != nil {
		t.Fatal(err)
	}

	_, errb, err := runCmdInConfigDir(t, configDir, "apps", "delete", "app_x", "--yes", "--no-input")
	if err != nil {
		t.Fatalf("--yes should let the delete proceed: %v\nstderr: %s", err, errb)
	}
	if !deleted {
		t.Fatal("--yes did not let the command reach the store delete call")
	}
}
