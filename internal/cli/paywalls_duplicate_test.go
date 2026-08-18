package cli_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/config"
)

func TestPaywallsDuplicate_CopiesComponentsWithDefaultName(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", configDir)

	var createBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/paywalls/pw_src"):
			_, _ = io.WriteString(w, `{"id":"pw_src","name":"Original","components":{"published":null,"draft":{"revision":2,"components_config":{"x":1},"components_localizations":{"en_US":{}},"default_locale":"en_US"}}}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/paywalls"):
			b, _ := io.ReadAll(r.Body)
			createBody = string(b)
			_, _ = io.WriteString(w, `{"id":"pw_copy","name":"Original (copy)"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	if err := config.Save("", &config.Config{APIKey: "sk_test", ProjectID: "proj", BaseURL: srv.URL}); err != nil {
		t.Fatal(err)
	}

	out, _, err := runCmdInConfigDir(t, configDir, "paywalls", "duplicate", "pw_src", "--no-input", "--json")
	if err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	if !strings.Contains(out, "pw_copy") {
		t.Fatalf("want the new paywall id in output: %s", out)
	}
	// The copy carries the source's components and a defaulted name.
	for _, want := range []string{`"name":"Original (copy)"`, `"components_config":{"x":1}`, `"default_locale":"en_US"`} {
		if !strings.Contains(createBody, want) {
			t.Errorf("create body missing %s:\n%s", want, createBody)
		}
	}
}

func TestPaywallsDuplicate_ErrorsWhenSourceHasNoComponents(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("RC_CONFIG_DIR", configDir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("no write should happen when the source has no components: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"pw_src","name":"Original","components":{"published":null,"draft":null}}`)
	}))
	t.Cleanup(srv.Close)
	if err := config.Save("", &config.Config{APIKey: "sk_test", ProjectID: "proj", BaseURL: srv.URL}); err != nil {
		t.Fatal(err)
	}

	_, _, err := runCmdInConfigDir(t, configDir, "paywalls", "duplicate", "pw_src", "--no-input", "--json")
	if err == nil || !strings.Contains(err.Error(), "no components") {
		t.Fatalf("want a no-components error, got: %v", err)
	}
}
