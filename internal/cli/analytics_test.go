package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
)

func TestCommandPath(t *testing.T) {
	root := &cobra.Command{Use: "rc"}
	paywalls := &cobra.Command{Use: "paywalls"}
	generate := &cobra.Command{Use: "generate"}
	paywalls.AddCommand(generate)
	root.AddCommand(paywalls)

	cases := map[*cobra.Command]string{
		root:     "",
		paywalls: "paywalls",
		generate: "paywalls.generate",
	}
	for cmd, want := range cases {
		if got := dottedCommandPath(cmd); got != want {
			t.Errorf("dottedCommandPath(%q) = %q, want %q", cmd.CommandPath(), got, want)
		}
	}
}

func TestCliMode(t *testing.T) {
	cases := []struct {
		name string
		ci   string
		g    Globals
		want string
	}{
		{"interactive", "", Globals{}, "interactive"},
		{"json is agent", "", Globals{JSON: true}, "agent"},
		{"no-input is agent", "", Globals{NoInput: true}, "agent"},
		{"ci wins over json", "true", Globals{JSON: true}, "ci"},
		{"ci any value", "1", Globals{}, "ci"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CI", tc.ci)
			if got := cliMode(&tc.g); got != tc.want {
				t.Errorf("cliMode = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDoNotTrack(t *testing.T) {
	cases := map[string]bool{"": false, "0": false, "false": false, "1": true, "true": true, "TRUE": true, " 1 ": true}
	for val, want := range cases {
		t.Setenv("DO_NOT_TRACK", val)
		if got := doNotTrack(); got != want {
			t.Errorf("doNotTrack() with DO_NOT_TRACK=%q = %v, want %v", val, got, want)
		}
	}
}

func TestRequestHeaders(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("DO_NOT_TRACK", "")
	t.Setenv("RC_HEADERS", "")

	h := requestHeaders(&Globals{CommandPath: "apps.apple.setup", JSON: true})
	if got := h.Get(headerCLICommand); got != "apps.apple.setup" {
		t.Errorf("%s = %q, want apps.apple.setup", headerCLICommand, got)
	}
	if got := h.Get(headerCLIMode); got != "agent" {
		t.Errorf("%s = %q, want agent", headerCLIMode, got)
	}
}

func TestRequestHeadersDoNotTrackDropsAnalytics(t *testing.T) {
	t.Setenv("DO_NOT_TRACK", "1")
	t.Setenv("RC_HEADERS", "X-Trace: keep-me")

	h := requestHeaders(&Globals{CommandPath: "paywalls.generate"})
	if got := h.Get(headerCLICommand); got != "" {
		t.Errorf("DO_NOT_TRACK must drop %s, got %q", headerCLICommand, got)
	}
	if got := h.Get(headerCLIMode); got != "" {
		t.Errorf("DO_NOT_TRACK must drop %s, got %q", headerCLIMode, got)
	}
	if got := h.Get("X-Trace"); got != "keep-me" {
		t.Errorf("DO_NOT_TRACK must not drop RC_HEADERS, X-Trace = %q, want keep-me", got)
	}
}

func TestUserAgentFormat(t *testing.T) {
	ua := userAgent("1.2.3")
	if !strings.HasPrefix(ua, "revenuecat-cli/1.2.3 (") {
		t.Errorf("User-Agent = %q, want revenuecat-cli/1.2.3 prefix", ua)
	}
	if !strings.Contains(ua, "; go") {
		t.Errorf("User-Agent = %q, want a go version segment", ua)
	}
}

// TestAnalyticsHeadersReachTheWire drives a real request through the API client
// built the same way Runtime.API() builds it, and asserts all three headers land.
func TestAnalyticsHeadersReachTheWire(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("DO_NOT_TRACK", "")
	t.Setenv("RC_HEADERS", "")

	var gotCmd, gotMode, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCmd = r.Header.Get(headerCLICommand)
		gotMode = r.Header.Get(headerCLIMode)
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	g := &Globals{CommandPath: "customers.show", NoInput: true, Version: "9.9.9"}
	c := api.NewClient(api.Options{
		APIKey:       "sk_test",
		BaseURL:      srv.URL,
		UserAgent:    userAgent(g.Version),
		ExtraHeaders: requestHeaders(g),
	})
	if _, _, err := c.Raw(context.Background(), http.MethodGet, "/anything", nil); err != nil {
		t.Fatal(err)
	}

	if gotCmd != "customers.show" {
		t.Errorf("%s = %q, want customers.show", headerCLICommand, gotCmd)
	}
	if gotMode != "agent" {
		t.Errorf("%s = %q, want agent", headerCLIMode, gotMode)
	}
	if !strings.HasPrefix(gotUA, "revenuecat-cli/9.9.9 (") {
		t.Errorf("User-Agent = %q, want revenuecat-cli/9.9.9 prefix", gotUA)
	}
}

func TestDoNotTrackStillSendsRequest(t *testing.T) {
	t.Setenv("DO_NOT_TRACK", "1")
	t.Setenv("RC_HEADERS", "")

	var gotCmd, gotMode, gotUA string
	var served bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = true
		gotCmd = r.Header.Get(headerCLICommand)
		gotMode = r.Header.Get(headerCLIMode)
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	g := &Globals{CommandPath: "customers.show", Version: "9.9.9"}
	c := api.NewClient(api.Options{
		APIKey:       "sk_test",
		BaseURL:      srv.URL,
		UserAgent:    userAgent(g.Version),
		ExtraHeaders: requestHeaders(g),
	})
	if _, _, err := c.Raw(context.Background(), http.MethodGet, "/anything", nil); err != nil {
		t.Fatal(err)
	}

	if !served {
		t.Fatal("request must still be made under DO_NOT_TRACK")
	}
	if gotCmd != "" || gotMode != "" {
		t.Errorf("DO_NOT_TRACK must drop analytics headers, got command=%q mode=%q", gotCmd, gotMode)
	}
	if !strings.HasPrefix(gotUA, "revenuecat-cli/") {
		t.Errorf("User-Agent must still be sent under DO_NOT_TRACK, got %q", gotUA)
	}
}
