package cli_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDestructiveCommandsRequireConsentBeforeMutation(t *testing.T) {
	cases := []struct {
		args      []string
		preflight string
		config    string
	}{
		{args: []string{"apps", "delete", "app_x"}},
		{args: []string{"paywalls", "delete", "pw_x"}},
		{args: []string{"offerings", "delete", "ofrng_x"}},
		{args: []string{"entitlements", "delete", "entl_x"}},
		{args: []string{"products", "delete", "prod_x"}},
		{args: []string{"packages", "delete", "pkg_x"}},
		{args: []string{"products", "push", "prod_x"}},
		{args: []string{"paywalls", "publish", "pw_x"}},
		{args: []string{"paywalls", "unpublish", "pw_x"}},
		{args: []string{"webhooks", "delete", "wh_x"}},
		{args: []string{"purchases", "refund", "txn_x"}},
		{args: []string{"subscriptions", "cancel", "sub_x"}},
		{args: []string{"subscriptions", "refund", "sub_x"}},
		{args: []string{"products", "store", "discard", "plan_x"}},
		{args: []string{"customers", "revoke", "cust_x", "entl_x"}},
		{args: []string{"customers", "grant", "cust_x", "entl_x", "--duration", "monthly"}},
		{args: []string{"customers", "transfer", "cust_x", "--to", "cust_y"}},
		{args: []string{"experiments", "delete", "exp_x"}, preflight: `{"id":"exp_x","status":"draft"}`},
		{args: []string{"experiments", "start", "exp_x"}, preflight: `{"id":"exp_x","status":"draft"}`},
		{args: []string{"experiments", "pause", "exp_x"}, preflight: `{"id":"exp_x","status":"running"}`},
		{args: []string{"experiments", "resume", "exp_x"}, preflight: `{"id":"exp_x","status":"paused"}`},
		{args: []string{"experiments", "stop", "exp_x"}, preflight: `{"id":"exp_x","status":"running"}`},
		{args: []string{"experiments", "update", "exp_x"}, preflight: `{"id":"exp_x","status":"running"}`, config: `{"enrollment_percentage":25}`},
		{args: []string{"targeting", "delete", "trle_x"}, preflight: `{"id":"trle_x","rule_type":"legacy","state":"inactive"}`},
		{args: []string{"targeting", "create", "--name", "Test", "--offering", "ofrng_x", "--state", "active"}},
		{args: []string{"targeting", "update", "trle_x"}, preflight: `{"id":"trle_x","rule_type":"legacy","state":"active"}`, config: `{"position":1}`},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			mutations := 0
			approved := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					if tc.preflight != "" {
						_, _ = io.WriteString(w, tc.preflight)
					} else {
						_, _ = io.WriteString(w, `{}`)
					}
					return
				}
				mutations++
				if !approved {
					t.Errorf("%s %s reached the API before consent", r.Method, r.URL.Path)
				}
				_, _ = io.WriteString(w, `{"id":"result","status":"running","state":"active"}`)
			}))
			t.Cleanup(server.Close)
			t.Setenv("RC_BASE_URL", server.URL)
			args := append([]string{}, tc.args...)
			if tc.config != "" {
				path := filepath.Join(t.TempDir(), "update.json")
				if err := os.WriteFile(path, []byte(tc.config), 0o600); err != nil {
					t.Fatal(err)
				}
				args = append(args, "--config", path)
			}
			args = append(args, "--project-id", "proj", "--api-key", "sk_test", "--no-input")
			for _, jsonMode := range []bool{false, true} {
				runArgs := append([]string{}, args...)
				if jsonMode {
					runArgs = append(runArgs, "--json")
				}
				_, _, err := runAgentCmd(t, runArgs...)
				if err == nil || !strings.Contains(err.Error(), "pass --yes") || mutations != 0 {
					t.Fatalf("want consent refusal without mutation: json=%v err=%v mutations=%d", jsonMode, err, mutations)
				}
			}
			approved = true
			_, stderr, err := runAgentCmd(t, append(args, "--yes", "--json")...)
			if err != nil || stderr != "" || mutations != 1 {
				t.Fatalf("explicit consent must allow one mutation: err=%v stderr=%s mutations=%d", err, stderr, mutations)
			}
		})
	}
}
