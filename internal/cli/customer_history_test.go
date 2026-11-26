package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCustomerHistoryPagination(t *testing.T) {
	for _, tt := range []struct {
		command string
		itemID  string
	}{
		{"subscriptions", "sub_first"},
		{"purchases", "pur_first"},
	} {
		t.Run(tt.command, func(t *testing.T) {
			nextPage := "/v2/projects/proj/customers/cust/" + tt.command + "?starting_after=next%2Bitem&limit=1"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if want := "/projects/proj/customers/cust/" + tt.command; r.Method != http.MethodGet || r.URL.Path != want {
					t.Errorf("unexpected request %s %s, want GET %s", r.Method, r.URL.Path, want)
				}
				if got := r.URL.Query().Get("limit"); got != "1" {
					t.Errorf("limit = %q, want 1", got)
				}
				w.Header().Set("Content-Type", "application/json")
				switch cursor := r.URL.Query().Get("starting_after"); cursor {
				case "":
					if err := json.NewEncoder(w).Encode(map[string]any{
						"object": "list", "items": []map[string]string{{"id": tt.itemID}}, "next_page": nextPage,
					}); err != nil {
						t.Error(err)
					}
				case "next+item":
					_, _ = io.WriteString(w, `{"object":"list","items":[],"next_page":null}`)
				default:
					t.Errorf("unexpected starting_after %q", cursor)
				}
			}))
			t.Cleanup(server.Close)

			out, stderr, err := runProjectSetupCommand(t, server.URL, "customers", tt.command, "cust", "--limit", "1", "--no-input", "--json")
			if err != nil || stderr != "" {
				t.Fatalf("first page: %v, stderr=%q", err, stderr)
			}
			var envelope struct {
				Data struct {
					Items    []struct{ ID string } `json:"items"`
					NextPage string                `json:"next_page"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(out), &envelope); err != nil {
				t.Fatal(err)
			}
			if len(envelope.Data.Items) != 1 || envelope.Data.Items[0].ID != tt.itemID || envelope.Data.NextPage != nextPage {
				t.Fatalf("unexpected first page: %s", out)
			}

			out, stderr, err = runProjectSetupCommand(t, server.URL, "customers", tt.command, "cust", "--limit", "1", "--no-input")
			if err != nil || !strings.Contains(out, tt.itemID) || !strings.Contains(stderr, "--cursor next+item") {
				t.Fatalf("human first page: %v, stdout=%q, stderr=%q", err, out, stderr)
			}
			out, stderr, err = runProjectSetupCommand(t, server.URL, "customers", tt.command, "cust", "--limit", "1", "--cursor", "next+item", "--no-input", "--json")
			if err != nil || stderr != "" || !strings.Contains(out, `"items": []`) || strings.Contains(out, `"next_page"`) {
				t.Fatalf("last page: %v, stdout=%q, stderr=%q", err, out, stderr)
			}
		})
	}
}

func TestCustomerHistoryDiscovery(t *testing.T) {
	commands, _, err := runCmd(t, "commands", "--json")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"subscriptions", "purchases"} {
		if !strings.Contains(commands, `"path": "rc customers `+name+`"`) {
			t.Errorf("rc commands is missing customers %s", name)
		}
		schema, _, err := runCmd(t, "schema", "customers", name, "--json")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"name": "cursor"`, `"name": "limit"`, `"name": "customer-id"`, `"required": true`} {
			if !strings.Contains(schema, want) {
				t.Errorf("rc schema customers %s is missing %s", name, want)
			}
		}
	}
}
