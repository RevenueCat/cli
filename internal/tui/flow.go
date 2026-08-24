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
// Interactive prompts (Confirm/Select/Input) happen between rail calls — they're
// the active moments the rail pauses for, then resumes. Shares its styles and
// rail helpers with the prompt models (prompt_rail.go).
//
// On a non-TTY (plain), the glyphs are dropped for clean append-only lines.
type Flow struct {
	w     io.Writer
	plain bool
}

// NewFlow builds a flow writing to w. plain drops the rail (non-TTY/CI).
func NewFlow(w io.Writer, plain bool) *Flow { return &Flow{w: w, plain: plain} }

// Intro opens the flow with a ┌ cap and title.
func (f *Flow) Intro(title string) {
	if f.plain {
		fmt.Fprintln(f.w, title)
		return
	}
	fmt.Fprintln(f.w, prActiveSty.Render("┌")+"  "+prTitleSty.Render(title))
	fmt.Fprintln(f.w, railSpacer())
}

// Step starts a new section with a ◇ marker, preceded by a rail spacer.
func (f *Flow) Step(title string) {
	if f.plain {
		fmt.Fprintf(f.w, "\n%s\n", title)
		return
	}
	fmt.Fprintln(f.w, railSpacer())
	fmt.Fprintln(f.w, prActiveSty.Render("◇")+"  "+prTitleSty.Render(title))
}

// Say prints a dim narration line on the rail.
func (f *Flow) Say(line string) { f.body(prDimSty.Render(line)) }

// Item prints a bulleted list item on the rail, indented under the current step.
func (f *Flow) Item(line string) {
	f.body(prRailSty.Render("•") + " " + prDimSty.Render(line))
}

// URL prints a full URL on the rail — clickable in capable terminals and
// selectable to copy everywhere.
func (f *Flow) URL(url string) {
	if f.plain {
		f.body(url)
		return
	}
	f.body(hyperlink(url, url))
}

// Receipt echoes a confirmed choice as "✓ key  value" on the rail.
func (f *Flow) Receipt(key, value string) {
	line := prOKSty.Render("✓") + " " + key
	if value != "" {
		line += "  " + prDimSty.Render(value)
	}
	f.body(line)
}

// Warn prints a warning line on the rail.
func (f *Flow) Warn(line string) { f.body(prWarnSty.Render("! " + line)) }

// Outro closes the flow with a └ cap; extras are indented under it.
func (f *Flow) Outro(title string, extras ...string) {
	if f.plain {
		fmt.Fprintln(f.w, title)
		for _, e := range extras {
			fmt.Fprintln(f.w, "  "+e)
		}
		return
	}
	fmt.Fprintln(f.w, railSpacer())
	fmt.Fprintln(f.w, prActiveSty.Render("└")+"  "+prTitleSty.Render(title))
	for _, e := range extras {
		fmt.Fprintln(f.w, "   "+e)
	}
}

// Ledger returns a step ledger whose lines sit on the flow's rail.
func (f *Flow) Ledger(labels ...string) *Ledger {
	l := NewLedger(f.w, f.plain, labels...)
	if !f.plain {
		l.gutter = railSpacer() + "  "
	}
	return l
}

func (f *Flow) body(s string) {
	if f.plain {
		fmt.Fprintln(f.w, "  "+s)
		return
	}
	fmt.Fprintln(f.w, railBody(s))
}

// hyperlink renders label as a clickable OSC 8 link to url, underlined in the
// brand accent.
func hyperlink(label, url string) string {
	return output.Hyperlink(lipgloss.NewStyle().Foreground(output.BrandRed).Underline(true).Render(label), url)
}
