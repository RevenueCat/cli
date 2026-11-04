package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCustomerBalancesPreservesWalletAlias(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/projects/proj/customers/cust/virtual_currencies" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"items":[{"currency_code":"GLD","balance":10}],"next_page":null}`)
	}))
	t.Cleanup(server.Close)
	t.Setenv("RC_BASE_URL", server.URL)
	var canonical string
	for _, verb := range []string{"balances", "wallet"} {
		out, stderr, err := runAgentCmd(t, "customers", verb, "cust", "--project-id", "proj", "--api-key", "sk_test", "--json", "--no-input")
		if err != nil || stderr != "" || !json.Valid([]byte(out)) {
			t.Fatalf("%s: err=%v stderr=%s out=%s", verb, err, stderr, out)
		}
		if verb == "balances" {
			canonical = out
		} else if out != canonical {
			t.Fatalf("wallet alias changed the response: got=%s want=%s", out, canonical)
		}
	}
	if requests != 2 {
		t.Fatalf("got %d requests, want 2", requests)
	}
}

func TestCustomerBalancesDiscoveryUsesCanonicalName(t *testing.T) {
	for _, name := range []string{"balances", "wallet"} {
		out, _, err := runCmd(t, "schema", "customers", name, "--json")
		if err != nil {
			t.Fatal(err)
		}
		var envelope struct {
			Data struct {
				Name    string   `json:"name"`
				Aliases []string `json:"aliases"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(out), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Data.Name != "balances" || len(envelope.Data.Aliases) != 1 || envelope.Data.Aliases[0] != "wallet" {
			t.Fatalf("unexpected discovery schema: %s", out)
		}
	}
}
