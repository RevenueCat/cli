package cli

import (
	"bytes"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

func TestGoogleSetupLinks(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	profile := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(profile) })
	sgr := regexp.MustCompile("\x1b\\[[0-9;]*m")
	const appURL = "https://app.revenuecat.com/projects/proj/apps/app"
	scenarios := []struct {
		name   string
		url    string
		render func(*tui.Flow)
	}{
		{"developer-account", "https://play.google.com/console/", showPlayDeveloperAccountInstructions},
		{"connected", appURL, func(fl *tui.Flow) {
			renderGoogleSetupResult(fl, googleSetupResult{CredentialUploaded: true}, appURL)
		}},
		{"credential-upload", appURL, func(fl *tui.Flow) {
			renderGoogleSetupResult(fl, googleSetupResult{KeyPath: "revenuecat-key.json"}, appURL)
		}},
	}
	for _, scenario := range scenarios {
		for _, mode := range []string{"color", "no-color", "plain", "quiet"} {
			t.Run(scenario.name+"/"+mode, func(t *testing.T) {
				lipgloss.SetColorProfile(termenv.TrueColor)
				if mode == "plain" {
					lipgloss.SetColorProfile(termenv.Ascii)
				}
				var buf bytes.Buffer
				renderer := output.NewRenderer(io.Discard, &buf, false, mode == "no-color", mode == "quiet", "")
				scenario.render(tui.NewFlow(renderer.Stderr(), mode == "plain", renderer.NoColor(), mode == "quiet"))
				got := buf.String()
				if mode == "quiet" {
					if got != "" {
						t.Fatalf("quiet output: %q", got)
					}
					return
				}
				if mode == "color" {
					open, close := "\x1b]8;;"+scenario.url+"\x1b\\", "\x1b]8;;\x1b\\"
					if strings.Count(got, open) != 1 || strings.Count(got, close) != 1 {
						t.Fatalf("expected one intact hyperlink to %s: %q", scenario.url, got)
					}
					if !sgr.MatchString(got) {
						t.Fatal("color was not enabled; this test would miss the original regression")
					}
					got = strings.ReplaceAll(strings.ReplaceAll(got, open, ""), close, "")
					got = sgr.ReplaceAllString(got, "")
				}
				if strings.ContainsAny(got, "\x1b\x07") || strings.Contains(got, "]8;;") {
					t.Fatalf("unexpected escape or broken hyperlink fragment: %q", got)
				}
				if !strings.Contains(got, scenario.url) {
					t.Fatalf("copyable URL missing: %q", got)
				}
			})
		}
	}
}
