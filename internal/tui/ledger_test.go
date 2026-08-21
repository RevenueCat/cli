package tui

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderLedger(t *testing.T) {
	steps := []ledgerStep{
		{label: "Enable APIs", status: ledgerDone},
		{label: "Create service account", status: ledgerRunning},
		{label: "Grant roles", note: "quota exceeded", status: ledgerFailed},
		{label: "Create key", status: ledgerPending},
	}
	out := renderLedger(steps, "* ", "  ")

	for _, want := range []string{"Enable APIs", "Create service account", "Grant roles", "Create key", "quota exceeded"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "✓") || !strings.Contains(out, "✗") || !strings.Contains(out, "○") {
		t.Errorf("render missing a status glyph:\n%s", out)
	}
	if lines := strings.Count(strings.TrimRight(out, "\n"), "\n") + 1; lines != len(steps) {
		t.Errorf("expected %d lines, got %d:\n%s", len(steps), lines, out)
	}
}

func TestLedgerPlain(t *testing.T) {
	var buf bytes.Buffer
	l := NewLedger(&buf, true, "Enable APIs", "Create key")
	l.Start() // no-op in plain mode
	l.Running(0)
	l.Done(0, "")
	l.Running(1)
	l.Fail(1, "denied")
	l.Stop()

	got := buf.String()
	if !strings.Contains(got, "✓ Enable APIs") {
		t.Errorf("plain output missing done line:\n%s", got)
	}
	if !strings.Contains(got, "✗ Create key  denied") {
		t.Errorf("plain output missing fail line:\n%s", got)
	}
	// Running is intentionally silent in plain mode (one line per step).
	if strings.Count(got, "\n") != 2 {
		t.Errorf("expected 2 lines in plain mode, got:\n%s", got)
	}
}
