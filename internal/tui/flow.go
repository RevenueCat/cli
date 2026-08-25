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
	w       io.Writer
	plain   bool
	noColor bool
	quiet   bool
	in      io.Reader // nil = the real terminal (os.Stdin); set by tests to script prompts
}

func NewFlow(w io.Writer, plain, noColor, quiet bool) *Flow {
	return &Flow{w: w, plain: plain, noColor: noColor, quiet: quiet}
}

func (f *Flow) Intro(title string) {
	if f.quiet {
		return
	}
	if f.plain {
		fmt.Fprintln(f.w, title)
		return
	}
	fmt.Fprintln(f.w, prActiveSty.Render("┌")+"  "+prTitleSty.Render(title))
	fmt.Fprintln(f.w, railSpacer())
}

func (f *Flow) Step(title string) {
	if f.quiet {
		return
	}
	if f.plain {
		fmt.Fprintf(f.w, "\n%s\n", title)
		return
	}
	fmt.Fprintln(f.w, railSpacer())
	fmt.Fprintln(f.w, prActiveSty.Render("◇")+"  "+prTitleSty.Render(title))
}

func (f *Flow) Say(line string) { f.body(prDimSty.Render(line)) }

func (f *Flow) Item(line string) {
	f.body(prRailSty.Render("•") + " " + prDimSty.Render(line))
}

// URL prints the raw URL, not a hyperlink label, so it stays selectable to copy
// on terminals that don't support OSC 8.
func (f *Flow) URL(url string) {
	if f.plain || f.noColor {
		f.body(url)
		return
	}
	f.body(hyperlink(url, url))
}

func (f *Flow) Receipt(key, value string) {
	line := prOKSty.Render("✓") + " " + key
	if value != "" {
		line += "  " + prDimSty.Render(value)
	}
	f.body(line)
}

func (f *Flow) Warn(line string) { f.body(prWarnSty.Render("! " + line)) }

// Hint prints a dim guidance line on the rail (rail-native equivalent of
// Renderer.Hint, so hints stay on the gutter mid-flow).
func (f *Flow) Hint(line string) { f.body(prDimSty.Render("→ " + line)) }

func (f *Flow) Outro(title string, extras ...string) {
	if f.quiet {
		return
	}
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

func (f *Flow) Ledger(labels ...string) *Ledger {
	if f.quiet {
		return NewLedger(io.Discard, true, labels...)
	}
	l := NewLedger(f.w, f.plain, labels...)
	if !f.plain {
		l.gutter = railSpacer() + "  "
	}
	return l
}

func (f *Flow) body(s string) {
	if f.quiet {
		return
	}
	if f.plain {
		fmt.Fprintln(f.w, "  "+s)
		return
	}
	fmt.Fprintln(f.w, railBody(s))
}

func hyperlink(label, url string) string {
	return output.Hyperlink(lipgloss.NewStyle().Foreground(output.BrandRed).Underline(true).Render(label), url)
}
