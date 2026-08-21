package tui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/lipgloss"

	"github.com/revenuecat/cli/internal/output"
)

// Flow renders a guided, single-experience command as one continuous vertical
// rail (clack-style): a ┌ intro cap, ◇ step headers and │ narration on a shared
// spine, and a └ outro cap. It's the shell for human-first `requires_human`
// flows so the whole command reads as one designed experience rather than a
// scatter of lines.
//
// Interactive prompts (tui.Form / decide / confirm) happen between rail calls —
// they're the active moments the rail pauses for, then resumes.
//
// On a non-TTY (plain), the glyphs are dropped for clean append-only lines.
type Flow struct {
	w     io.Writer
	plain bool
}

var (
	flowRailSty  = lipgloss.NewStyle().Foreground(output.BrandRed)
	flowCapSty   = lipgloss.NewStyle().Foreground(output.BrandRed).Bold(true)
	flowTitleSty = lipgloss.NewStyle().Bold(true)
	flowDimSty   = lipgloss.NewStyle().Faint(true)
	flowOKSty    = lipgloss.NewStyle().Foreground(output.GreenOK).Bold(true)
	flowWarnSty  = lipgloss.NewStyle().Foreground(output.WarnAmber)
)

// NewFlow builds a flow writing to w. plain drops the rail (non-TTY/CI).
func NewFlow(w io.Writer, plain bool) *Flow { return &Flow{w: w, plain: plain} }

func (f *Flow) rail() string { return flowRailSty.Render("│") }

// Intro opens the flow with a ┌ cap and title.
func (f *Flow) Intro(title string) {
	if f.plain {
		fmt.Fprintln(f.w, title)
		return
	}
	fmt.Fprintln(f.w, flowCapSty.Render("┌")+"  "+flowTitleSty.Render(title))
	fmt.Fprintln(f.w, f.rail())
}

// Step starts a new section with a ◇ marker, preceded by a rail spacer.
func (f *Flow) Step(title string) {
	if f.plain {
		fmt.Fprintf(f.w, "\n%s\n", title)
		return
	}
	fmt.Fprintln(f.w, f.rail())
	fmt.Fprintln(f.w, flowCapSty.Render("◇")+"  "+flowTitleSty.Render(title))
}

// Say prints a dim narration line on the rail.
func (f *Flow) Say(line string) { f.body(flowDimSty.Render(line)) }

// Link prints a narration line ending in a clickable label.
func (f *Flow) Link(text, label, url string) {
	f.body(flowDimSty.Render(text) + " " + linkText(label, url))
}

// Receipt echoes a confirmed choice as "✓ key  value" on the rail.
func (f *Flow) Receipt(key, value string) {
	line := flowOKSty.Render("✓") + " " + key
	if value != "" {
		line += "  " + flowDimSty.Render(value)
	}
	f.body(line)
}

// Warn prints a warning line on the rail.
func (f *Flow) Warn(line string) { f.body(flowWarnSty.Render("! " + line)) }

// Outro closes the flow with a └ cap; extras are indented under it.
func (f *Flow) Outro(title string, extras ...string) {
	if f.plain {
		fmt.Fprintln(f.w, title)
		for _, e := range extras {
			fmt.Fprintln(f.w, "  "+e)
		}
		return
	}
	fmt.Fprintln(f.w, f.rail())
	fmt.Fprintln(f.w, flowCapSty.Render("└")+"  "+flowTitleSty.Render(title))
	for _, e := range extras {
		fmt.Fprintln(f.w, "   "+e)
	}
}

// Ledger returns a step ledger whose lines sit on the flow's rail.
func (f *Flow) Ledger(labels ...string) *Ledger {
	l := NewLedger(f.w, f.plain, labels...)
	if !f.plain {
		l.gutter = f.rail() + "  "
	}
	return l
}

func (f *Flow) body(s string) {
	if f.plain {
		fmt.Fprintln(f.w, "  "+s)
		return
	}
	fmt.Fprintln(f.w, f.rail()+"  "+s)
}

// linkText mirrors output.Renderer.LinkText for use inside the flow (OSC 8
// clickable label; plain-ish fallback when styling is unavailable is handled by
// the terminal ignoring the escapes).
func linkText(label, url string) string {
	styled := lipgloss.NewStyle().Foreground(output.BrandRed).Underline(true).Render(label)
	return "\x1b]8;;" + url + "\x1b\\" + styled + "\x1b]8;;\x1b\\"
}
