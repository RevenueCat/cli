package tui

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"unicode"
)

// The Flow rail renders values this process didn't author — app names and
// error strings from the API, team and account names from Apple and Google,
// the working directory's base name. A raw escape in any of them would address
// the operator's terminal (set the window title, write the clipboard with
// OSC 52, clear the screen), so every rail writer keeps its text to visible
// characters, exactly like the renderer and the browser do.
func TestFlowWritersRenderValuesAsVisibleText(t *testing.T) {
	const (
		osc52 = "\x1b]52;c;cGF5bG9hZA==\x07" // clipboard write
		osc0  = "\x1b]0;window-title\x07"
		csi   = "\x1b[2J"
	)
	for _, plain := range []bool{false, true} {
		asciiProfile(t)
		var buf bytes.Buffer
		fl := &Flow{w: &buf, plain: plain}
		fl.Intro("App Store Connect check — Pro" + osc52 + "App")
		fl.Step("Current configuration" + csi)
		fl.Say("Signing in as a" + osc0 + "b…")
		fl.Item("Vendor number · 12" + csi + "34")
		fl.Receipt("Team"+csi, "Acme"+osc52)
		fl.Warn("Couldn't fetch it automatically: " + osc0)
		fl.Hint("try this" + csi)
		fl.Outro("Partly done"+csi, "extra "+osc0)

		// asciiProfile keeps lipgloss from emitting styling escapes, so any
		// ESC or BEL left in the output came from the values above.
		if out := buf.String(); strings.ContainsAny(out, "\x1b\x07") {
			t.Errorf("plain=%v: rail emitted raw control bytes from a value: %q", plain, out)
		}
		out := buf.String()
		for _, want := range []string{"App Store Connect check", "Vendor number", "Acme", "Partly done"} {
			if !strings.Contains(out, want) {
				t.Errorf("plain=%v: rail dropped the visible text %q: %q", plain, want, out)
			}
		}
	}
}

// URL is the one writer that emits an escape of its own (OSC 8). The URL is
// both the target and the label there, and Hyperlink can only sanitize the
// target, so the label has to arrive clean.
func TestFlowURLCannotBreakOutOfTheHyperlink(t *testing.T) {
	asciiProfile(t)
	const bad = "https://example.com/\x1b]52;c;cGF5bG9hZA==\x07x"

	var raw bytes.Buffer
	(&Flow{w: &raw, noColor: true}).URL(bad)
	if strings.ContainsAny(raw.String(), "\x1b\x07") {
		t.Errorf("no-color URL emitted raw control bytes: %q", raw.String())
	}

	var linked bytes.Buffer
	(&Flow{w: &linked}).URL(bad)
	out := linked.String()
	if strings.Count(out, "\x1b]8;;") != 2 || strings.Contains(out, "\x07") {
		t.Errorf("URL broke out of the OSC 8 sequence: %q", out)
	}
}

// A value that fakes a line break would push the rest of the line off the
// gutter, so the rail collapses them the same way table cells do.
func TestFlowKeepsValuesOnOneLine(t *testing.T) {
	asciiProfile(t)
	var buf bytes.Buffer
	fl := &Flow{w: &buf}
	fl.Say("row1\u2028row2")
	fl.Item("a\nb")
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if !strings.HasPrefix(line, "│") {
			t.Errorf("value broke off the gutter: %q\nfull: %q", line, buf.String())
		}
	}
}

// Bidi overrides are invisible but reverse the glyphs that follow, so a stored
// name carrying one makes the rail display text that reads differently from
// the value it came from.
func TestFlowStripsBidiOverrides(t *testing.T) {
	asciiProfile(t)
	var buf bytes.Buffer
	fl := &Flow{w: &buf, plain: true}
	fl.Intro("App Store Connect check — Premium \u202eEVIL")
	fl.Say("\u2066admin\u2069")
	out := buf.String()
	for _, bad := range []string{"\u202e", "\u2066", "\u2069"} {
		if strings.Contains(out, bad) {
			t.Errorf("rail emitted bidi control %q: %q", bad, out)
		}
	}
	if !strings.Contains(out, "Premium EVIL") || !strings.Contains(out, "admin") {
		t.Errorf("rail dropped the visible text: %q", out)
	}
}

func TestLedgerRendersNotesAsVisibleText(t *testing.T) {
	var buf bytes.Buffer
	l := NewLedger(&buf, true, "Upload the credential\x1b[2J")
	l.Fail(0, "RevenueCat didn't accept it: \x1b]0;t\x07")
	if out := buf.String(); strings.ContainsAny(out, "\x1b\x07") {
		t.Errorf("ledger emitted raw control bytes: %q", out)
	}
}

func TestFlowWritersHandleUnicode(t *testing.T) {
	asciiProfile(t)
	const bad = "a\u200e\u200f\u202e\u2066\u2069\u200bb\u2060c\u2028d\u2029e\nf\tg"
	const want = `a\u200bb\u2060c d e f g`
	writers := map[string]func(*Flow){
		"intro":         func(f *Flow) { f.Intro(bad) },
		"step":          func(f *Flow) { f.Step(bad) },
		"say":           func(f *Flow) { f.Say(bad) },
		"item":          func(f *Flow) { f.Item(bad) },
		"url":           func(f *Flow) { f.URL(bad) },
		"receipt key":   func(f *Flow) { f.Receipt(bad, "value") },
		"receipt value": func(f *Flow) { f.Receipt("key", bad) },
		"warn":          func(f *Flow) { f.Warn(bad) },
		"hint":          func(f *Flow) { f.Hint(bad) },
		"outro":         func(f *Flow) { f.Outro(bad) },
		"outro extra":   func(f *Flow) { f.Outro("done", bad) },
		"ledger label":  func(f *Flow) { f.Ledger(bad).Done(0, "") },
		"ledger note":   func(f *Flow) { f.Ledger("step").Fail(0, bad) },
	}
	for name, write := range writers {
		for _, plain := range []bool{true, false} {
			if strings.HasPrefix(name, "ledger") && !plain {
				continue
			}
			t.Run(fmt.Sprintf("%s/plain=%v", name, plain), func(t *testing.T) {
				var buf bytes.Buffer
				f := NewFlow(&buf, plain, true, false)
				write(f)
				got := buf.String()
				if !strings.Contains(got, want) {
					t.Fatalf("visible Unicode output missing: %q", got)
				}
				for _, r := range got {
					if unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
						t.Errorf("raw U+%04X survived: %q", r, got)
					}
				}
			})
		}
	}
}

func TestRailSelectLabelsStayOnOneLine(t *testing.T) {
	asciiProfile(t)
	for _, done := range []bool{false, true} {
		m := selectModel{opts: []Option{{Label: "a\nb\tc\u2028d\u2029e\u200bf\u200eg", Value: "unchanged\u202e"}}, done: done}
		got := m.View()
		if !strings.Contains(got, `a b c d e\u200bfg`) {
			t.Fatalf("unsafe select label: %q", got)
		}
		if m.opts[0].Value != "unchanged\u202e" {
			t.Fatal("display sanitization changed the selected value")
		}
	}
}
