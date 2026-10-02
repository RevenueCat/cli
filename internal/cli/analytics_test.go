package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/paywallai"
	"github.com/revenuecat/cli/internal/rico"
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

func TestNonV2AnalyticsHeadersReachTheWire(t *testing.T) {
	cases := []struct {
		name        string
		globals     Globals
		ci          string
		doNotTrack  string
		custom      string
		wantMode    string
		wantCommand string
		wantUA      string
	}{
		{name: "interactive", wantMode: "interactive", wantCommand: "auth.signup"},
		{name: "json", globals: Globals{JSON: true}, wantMode: "agent", wantCommand: "auth.signup"},
		{name: "no-input", globals: Globals{NoInput: true}, wantMode: "agent", wantCommand: "auth.signup"},
		{name: "ci", globals: Globals{JSON: true}, ci: "true", wantMode: "ci", wantCommand: "auth.signup"},
		{name: "do-not-track", doNotTrack: "1", custom: "X-Trace: keep-me"},
		{name: "overrides", custom: "User-Agent: custom-ua\nX-RC-CLI-Command: custom.command\nX-RC-CLI-Mode: custom-mode\nAuthorization: Bearer override", wantMode: "custom-mode", wantCommand: "custom.command", wantUA: "custom-ua"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CI", tc.ci)
			t.Setenv("DO_NOT_TRACK", tc.doNotTrack)
			t.Setenv("RC_HEADERS", tc.custom)
			tc.globals.CommandPath = "auth.signup"
			tc.globals.Version = "9.9.9"
			wantUA := tc.wantUA
			if wantUA == "" {
				wantUA = userAgent(tc.globals.Version)
			}
			var paths []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				for header, want := range map[string]string{
					"User-Agent":     wantUA,
					headerCLICommand: tc.wantCommand,
					headerCLIMode:    tc.wantMode,
				} {
					if got := r.Header.Get(header); got != want {
						t.Errorf("%s %s = %q, want %q", r.URL.Path, header, got, want)
					}
				}
				if tc.doNotTrack != "" && r.Header.Get("X-Trace") != "keep-me" {
					t.Errorf("%s lost custom header", r.URL.Path)
				}
				wantAuth := "Bearer access"
				if r.URL.Path == "/oauth2/token" {
					wantAuth = ""
				}
				if r.URL.Path == "/v1/subscribers/user/offerings" || r.URL.Path == "/v1/receipts" {
					wantAuth = "Bearer public-key"
				}
				if got := r.Header.Get("Authorization"); got != wantAuth {
					t.Errorf("%s Authorization = %q, want %q", r.URL.Path, got, wantAuth)
				}
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/oauth2/token" {
					_, _ = w.Write([]byte(`{"access_token":"access","refresh_token":"refresh","expires_in":3600}`))
				} else {
					_, _ = w.Write([]byte(`{}`))
				}
			}))
			t.Cleanup(srv.Close)
			t.Setenv("RC_OAUTH_BASE_URL", srv.URL)
			rt := &Runtime{Globals: &tc.globals, Config: &config.Config{AccessToken: "access", TokenType: "oauth"}}
			ctx := context.Background()
			if _, err := rt.oauthService().Refresh(ctx, "refresh"); err != nil {
				t.Fatal(err)
			}
			sdk := api.NewSDKService(api.SDKOptions{BaseURL: srv.URL + "/v2", UserAgent: userAgent(rt.Globals.Version), ExtraHeaders: requestHeaders(rt.Globals)})
			if _, err := sdk.Offerings(ctx, "public-key", "user"); err != nil {
				t.Fatal(err)
			}
			if _, err := sdk.SimulatePurchase(ctx, "public-key", api.SimulatedPurchase{}); err != nil {
				t.Fatal(err)
			}
			rc, err := ricoClient(rt, srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			if err := rc.PostFeedback(ctx, rico.FeedbackRequest{}); err != nil {
				t.Fatal(err)
			}
			rs, err := rc.Stream(ctx, rico.RunAgentInput{})
			if err != nil {
				t.Fatal(err)
			}
			if err := rs.Close(); err != nil {
				t.Fatal(err)
			}
			pc, err := paywallAIClient(rt, srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			if err := pc.Feedback(ctx, "session", "trace", "positive"); err != nil {
				t.Fatal(err)
			}
			ps, err := pc.Stream(ctx, paywallai.EditorRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if err := ps.Close(); err != nil {
				t.Fatal(err)
			}
			if len(paths) != 7 {
				t.Fatalf("requests = %v, want 7 requests", paths)
			}
		})
	}
}
