package cli_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A Customer's ID is whatever App User ID the app handed RevenueCat, and the
// v2 schema constrains it only by length (docs/specs/v2-developer.yaml —
// `Customer.id`: string, maxLength 1500, no pattern). So an ID can carry
// terminal control bytes, and human-mode output must show them rather than let
// the terminal act on them: OSC 52 replaces the reader's clipboard.
const osc52CustomerID = "rcbb_target\x1b]52;c;UkNCQjE5MQ==\x07"

// JSON-encoded form of the same ID, as the API would send it on the wire.
const osc52CustomerIDJSON = `rcbb_target\u001b]52;c;UkNCQjE5MQ==\u0007`

func customerEscapeServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		customer := `{"object":"customer","id":"` + osc52CustomerIDJSON + `","project_id":"proj_esc",` +
			`"first_seen_at":1700000000000,"last_seen_at":1700000000000,"last_seen_platform":"ios",` +
			`"last_seen_country":"` + osc52CustomerIDJSON + `","last_seen_app_version":"1.0.0",` +
			`"active_entitlements":{"object":"list","items":[{"object":"customer.active_entitlement","entitlement_id":"` + osc52CustomerIDJSON + `"}]}}`
		switch {
		case strings.HasSuffix(r.URL.Path, "/subscriptions"):
			io.WriteString(w, `{"object":"list","items":[{"object":"subscription","id":"`+osc52CustomerIDJSON+`","store":"app_store","status":"active"}]}`)
		case strings.HasSuffix(r.URL.Path, "/purchases"):
			io.WriteString(w, `{"object":"list","items":[]}`)
		case strings.HasSuffix(r.URL.Path, "/customers"):
			io.WriteString(w, `{"object":"list","items":[`+customer+`],"next_page":"/v2/projects/proj_esc/customers?starting_after=x"}`)
		default:
			io.WriteString(w, customer)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestCustomers_HumanOutputNeverLetsACustomerIDDriveTheTerminal(t *testing.T) {
	server := customerEscapeServer(t)
	t.Setenv("RC_BASE_URL", server.URL)

	for _, args := range [][]string{
		{"customers", "list"},
		{"customers", "show", "cus_escape"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			args = append(args, "--no-input", "--no-color", "--project-id", "proj_esc", "--api-key", "sk_esc")
			stdout, stderr, err := runAgentCmd(t, args...)
			if err != nil {
				t.Fatalf("execute: %v\nstderr: %s", err, stderr)
			}
			for name, stream := range map[string]string{"stdout": stdout, "stderr": stderr} {
				if strings.ContainsRune(stream, 0x1b) || strings.ContainsRune(stream, 0x07) {
					t.Errorf("%s hands the terminal a control sequence from the customer ID:\n%q", name, stream)
				}
			}
			if !strings.Contains(stdout, `\x1b]52`) {
				t.Errorf("the ID should still be readable as an escaped literal:\n%s", stdout)
			}
		})
	}
}

// The agent contract is unchanged: --json carries the exact bytes, encoded.
func TestCustomersList_JSONStillCarriesTheRawIDEncoded(t *testing.T) {
	server := customerEscapeServer(t)
	t.Setenv("RC_BASE_URL", server.URL)

	stdout, _, err := runAgentCmd(t, "customers", "list", "--json", "--no-input",
		"--project-id", "proj_esc", "--api-key", "sk_esc")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.ContainsRune(stdout, 0x1b) {
		t.Errorf("--json leaked a raw escape byte:\n%q", stdout)
	}
	if !strings.Contains(stdout, osc52CustomerIDJSON) {
		t.Errorf("--json must keep the exact ID:\n%s", stdout)
	}
}
