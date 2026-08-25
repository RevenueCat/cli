package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// asciiProfile makes lipgloss emit no ANSI so rendered output is deterministic
// to assert on.
func asciiProfile(t *testing.T) {
	t.Helper()
	lipgloss.SetColorProfile(termenv.Ascii)
}

// TestFlowStaysOnTheGutter is the guard against the "rail disappears" class of
// regression: every line the Flow emits between the intro and outro must carry a
// rail marker (┌ │ ◇ └) or be an outro extra, never a bare off-rail line.
func TestFlowStaysOnTheGutter(t *testing.T) {
	asciiProfile(t)
	var buf bytes.Buffer
	fl := &Flow{w: &buf}
	fl.Intro("Setup")
	fl.Say("lead line")
	fl.Receipt("Project", "Moodly")
	fl.Item("a bullet")
	fl.Step("Sign in")
	fl.Warn("careful")
	fl.Hint("try this")
	fl.Outro("Done", "one more thing")

	out := buf.String()
	for _, want := range []string{"┌  Setup", "◇  Sign in", "└  Done"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		onRail := false
		for _, p := range []string{"┌", "│", "◇", "└", " "} { // glyphs, gutter, or outro-extra indent
			if strings.HasPrefix(line, p) {
				onRail = true
				break
			}
		}
		if !onRail {
			t.Errorf("line is off the gutter: %q\nfull:\n%s", line, out)
		}
	}
}

func TestFlowQuietSuppressesEverything(t *testing.T) {
	asciiProfile(t)
	var buf bytes.Buffer
	fl := &Flow{w: &buf, quiet: true}
	fl.Intro("Setup")
	fl.Say("lead")
	fl.Step("Sign in")
	fl.Receipt("Project", "Moodly")
	fl.Warn("careful")
	fl.Outro("Done")
	if buf.Len() != 0 {
		t.Errorf("quiet flow should emit nothing, got:\n%q", buf.String())
	}
}

func TestFlowPlainDropsGlyphs(t *testing.T) {
	asciiProfile(t)
	var buf bytes.Buffer
	fl := &Flow{w: &buf, plain: true}
	fl.Intro("Setup")
	fl.Step("Sign in")
	out := buf.String()
	for _, glyph := range []string{"┌", "│", "◇", "└"} {
		if strings.Contains(out, glyph) {
			t.Errorf("plain mode should drop %q, got:\n%s", glyph, out)
		}
	}
	if !strings.Contains(out, "Setup") || !strings.Contains(out, "Sign in") {
		t.Errorf("plain mode should still print the text, got:\n%s", out)
	}
}

func TestFlowURLHonorsNoColor(t *testing.T) {
	asciiProfile(t)
	const url = "https://example.com/x"
	// noColor: raw URL, no OSC 8 hyperlink escape.
	var nc bytes.Buffer
	(&Flow{w: &nc, noColor: true}).URL(url)
	if strings.Contains(nc.String(), "\x1b]8;") {
		t.Errorf("no-color URL must not emit an OSC 8 escape, got:\n%q", nc.String())
	}
	if !strings.Contains(nc.String(), url) {
		t.Errorf("URL text missing:\n%q", nc.String())
	}
}
