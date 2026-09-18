package tui

import (
	"strings"
	"testing"
)

// The interactive browser draws the same remote strings the non-interactive
// renderer does — a customer ID carrying OSC 52 would replace the reader's
// clipboard the moment the list paints.
const browserOSC52 = "rcbb_target\x1b]52;c;UkNCQjE5MQ==\x07"

func TestBrowserViews_NeverDrawRemoteControlSequences(t *testing.T) {
	item := BrowserItem{
		ID:     browserOSC52,
		Label:  browserOSC52,
		Meta:   browserOSC52,
		Row:    []string{browserOSC52, "ios"},
		Fields: []BrowserField{{Key: "ID", Value: browserOSC52}},
		Links:  []BrowserLink{{Label: browserOSC52}},
	}
	sections := []BrowserSection{{
		Title: browserOSC52,
		Cols:  []string{"PRODUCT", "STORE"},
		Rows:  []BrowserSectionRow{{Cells: []string{browserOSC52, "app_store"}}},
	}}

	frames := map[string]bframe{
		"list":   newListFrame(browserOSC52, []BrowserItem{item}),
		"table":  newTableFrame(browserOSC52, []string{"ID", "PLATFORM"}, []BrowserItem{item}),
		"detail": newDetailFrame(item),
	}
	for name, frame := range frames {
		t.Run(name, func(t *testing.T) {
			m := &browser{stack: []bframe{frame}, width: 120, height: 40}
			// Sub-resources arrive asynchronously, after the frame was built.
			m.Update(autoLoadedMsg{frameIdx: 0, sections: sections})
			view := m.View()
			if strings.Contains(view, "\x1b]52") || strings.ContainsRune(view, 0x07) {
				t.Errorf("%s view draws the clipboard sequence:\n%q", name, view)
			}
			if !strings.Contains(view, `\x1b]52`) {
				t.Errorf("%s view should show the value as an escaped literal:\n%s", name, view)
			}
		})
	}
}

func TestBrowserErrors_NeverDrawRemoteControlSequences(t *testing.T) {
	frame := newListFrame("Customers", nil)
	frame.autoErr = "server said: " + browserOSC52
	m := &browser{stack: []bframe{frame}, width: 120, height: 40, loadErr: "server said: " + browserOSC52}
	if view := m.View(); strings.Contains(view, "\x1b]52") {
		t.Errorf("error view draws the clipboard sequence:\n%q", view)
	}
}
