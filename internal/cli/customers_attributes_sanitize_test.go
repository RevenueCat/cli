package cli_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode"
)

func TestCustomerAttributes_UnicodeDisplaySafety(t *testing.T) {
	const value = "Pro\u202eEVIL\u200e\u200f ad\u200bmin no\u2060break\u2028next\u2029end\U000e0001"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/proj/customers/customer/attributes" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list", "items": []any{map[string]string{"name": "$note", "value": value}},
		})
	}))
	t.Cleanup(server.Close)

	for _, mode := range []string{"human", "json", "filtered", "filtered string", "raw"} {
		t.Run(mode, func(t *testing.T) {
			args := []string{"customers", "attributes", "customer", "--no-color", "--no-input"}
			switch mode {
			case "json":
				args = append(args, "--json")
			case "filtered":
				args = append(args, "--json", "--format", ".data.items[]")
			case "filtered string":
				args = append(args, "--json", "--format", ".data.items[].value")
			case "raw":
				args = []string{"api", "GET", "/projects/proj/customers/customer/attributes", "--no-input"}
			}
			out, _, err := runProjectSetupCommand(t, server.URL, args...)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range out {
				if unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
					t.Errorf("%s output emitted raw U+%04X: %q", mode, r, out)
				}
			}
			if mode == "human" {
				// Long attribute arrays are summarized; short ones are displayed.
				if !strings.Contains(out, "(1 items)") || strings.Contains(out, "Pro") {
					t.Fatalf("human mode should summarize the attribute count: %q", out)
				}
				return
			}
			if !strings.Contains(out, `\u200b`) || !strings.Contains(out, `\u2060`) {
				t.Fatalf("invisible characters were not made visible: %q", out)
			}
			if mode == "filtered string" {
				if !strings.Contains(out, "ProEVIL") {
					t.Fatalf("visible attribute text lost: %q", out)
				}
				return
			}
			var decoded struct {
				Data struct {
					Items []struct {
						Value string `json:"value"`
					} `json:"items"`
				} `json:"data"`
				Items []struct {
					Value string `json:"value"`
				} `json:"items"`
				Value string `json:"value"`
			}
			if err := json.Unmarshal([]byte(out), &decoded); err != nil {
				t.Fatal(err)
			}
			got := decoded.Value
			if mode == "json" && len(decoded.Data.Items) == 1 {
				got = decoded.Data.Items[0].Value
			}
			if mode == "raw" && len(decoded.Items) == 1 {
				got = decoded.Items[0].Value
			}
			if got != value {
				t.Fatalf("%s changed attribute value: got %q, want %q", mode, got, value)
			}
		})
	}
}

func TestCustomerAttributes_ShortHumanValuesAreSanitized(t *testing.T) {
	for _, test := range []struct {
		name, value, want string
	}{
		{"bidi override", "Pro\u202eEVIL", "ProEVIL"},
		{"bidi marks", "a\u200eb\u200fc", "abc"},
		{"zero-width space", "ad\u200bmin", `ad\u200bmin`},
		{"word joiner", "no\u2060break", `no\u2060break`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/projects/proj/customers/customer/attributes" {
					t.Errorf("unexpected path: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"object": "list", "items": []any{map[string]string{"name": "$note", "value": test.value}},
				})
			}))
			t.Cleanup(server.Close)
			out, _, err := runProjectSetupCommand(t, server.URL, "customers", "attributes", "customer", "--no-color", "--no-input")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, test.want) || strings.Contains(out, "(1 items)") {
				t.Fatalf("short attribute value should be displayed safely: %q", out)
			}
		})
	}
}
