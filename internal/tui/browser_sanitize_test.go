package tui

import (
	"strings"
	"testing"
)

// Every list, table, and detail path renders values through brTrunc or an
// explicit Sanitize call; brTrunc is the shared chokepoint.
func TestBrTrunc_StripsControlBytes(t *testing.T) {
	got := brTrunc("cus_\x1b]52;c;UkNCQg==\x07x", 80)
	if strings.ContainsAny(got, "\x1b\x07") {
		t.Errorf("brTrunc left raw control bytes: %q", got)
	}
	if got != "cus_]52;c;UkNCQg==x" {
		t.Errorf("brTrunc = %q", got)
	}
	if short := brTrunc("plain", 3); short != "plain" {
		t.Errorf("short passthrough broken: %q", short)
	}
}
