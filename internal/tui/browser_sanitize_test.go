package tui

import (
	"strings"
	"testing"
)

// List, table, and section cells render through brTrunc; detail fields,
// breadcrumbs, and error lines carry their own Sanitize calls in the view code.
func TestBrTrunc_StripsControlBytes(t *testing.T) {
	got := brTrunc("id_\x1b]0;title\x07x", 80)
	if strings.ContainsAny(got, "\x1b\x07") {
		t.Errorf("brTrunc left raw control bytes: %q", got)
	}
	if got != "id_]0;titlex" {
		t.Errorf("brTrunc = %q", got)
	}
	if short := brTrunc("plain", 3); short != "plain" {
		t.Errorf("short passthrough broken: %q", short)
	}
	if multi := brTrunc("a\nb", 80); multi != "a b" {
		t.Errorf("newline should collapse to a space in cells, got %q", multi)
	}
}

// Breadcrumbs carry API values (frame titles, detail IDs) and must be
// sanitized inside renderHeader, not by each caller.
func TestRenderHeader_SanitizesCrumbsAndTitle(t *testing.T) {
	m := &browser{width: 80, stack: []bframe{
		newListFrame("Customers\x1b[2J", nil),
		newDetailFrame(BrowserItem{ID: "id_\x1b]0;t\x07x"}),
		newListFrame("child", nil),
	}}
	got := m.renderHeader("Current\x07Title")
	if strings.ContainsAny(got[1:], "\x07") || strings.Contains(got[1:], "\x1b]") || strings.Contains(got[1:], "\x1b[2J") {
		t.Errorf("header contains raw control bytes from API values: %q", got)
	}
}
