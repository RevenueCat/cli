package output

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Card is a structured detail view used by commands like `rc customer show`.
// It's an alternative to RenderTable for "one entity, many facets" output.
// JSON mode bypasses Card entirely and emits Raw via Render().
//
// Sections render in the order they're added. Empty sections are skipped so
// the output adapts when subresources are missing.
type Card struct {
	// Title is the bold one-liner at the top (e.g. "cus_abc · iOS · US").
	Title string
	// Subtitle is a dimmer second line (e.g. "first seen 2024-12-18 · last seen 2025-05-15").
	Subtitle string
	// Sections render in order. Use Chips for entitlements, Table for
	// subscriptions/purchases, Lines for arbitrary k:v pairs.
	Sections []CardSection
	// Raw is the structured payload returned under --json.
	Raw any
}

type CardSection struct {
	Heading string
	Chips   []Chip     // colored badges for short labels
	Table   *CardTable // for tabular data inside the card
	Lines   []CardLine // for k: v pairs
	Empty   string     // shown (dimmed) when the section has no content
}

type Chip struct {
	Label string
	Tone  ChipTone
}

type ChipTone int

const (
	ToneNeutral ChipTone = iota
	ToneActive
	ToneArchived
	ToneExpired
	ToneTrial
	ToneWarning
)

type CardTable struct {
	Columns []string
	Rows    [][]string
}

type CardLine struct {
	Key   string
	Value string
}

// RenderCard writes a Card to stdout in TTY mode or hands off to Render() in
// JSON mode. Status helpers continue to go to stderr.
func (r *Renderer) RenderCard(c Card) error {
	if r.json {
		return r.Render(c.Raw)
	}

	titleStyle := lipgloss.NewStyle().Bold(true)
	subtitleStyle := lipgloss.NewStyle().Faint(true)
	headingStyle := lipgloss.NewStyle().Bold(true).Underline(true)
	emptyStyle := lipgloss.NewStyle().Faint(true).Italic(true)

	if c.Title != "" {
		fmt.Fprintln(r.stdout, r.style(titleStyle, c.Title))
	}
	if c.Subtitle != "" {
		fmt.Fprintln(r.stdout, r.style(subtitleStyle, c.Subtitle))
	}

	for _, s := range c.Sections {
		fmt.Fprintln(r.stdout)
		if s.Heading != "" {
			fmt.Fprintln(r.stdout, r.style(headingStyle, s.Heading))
		}
		switch {
		case len(s.Chips) > 0:
			r.writeChips(s.Chips)
		case s.Table != nil && len(s.Table.Rows) > 0:
			r.writeCardTable(*s.Table)
		case len(s.Lines) > 0:
			r.writeLines(s.Lines)
		default:
			msg := s.Empty
			if msg == "" {
				msg = "none"
			}
			fmt.Fprintln(r.stdout, r.style(emptyStyle, "  "+msg))
		}
	}
	return nil
}

func (r *Renderer) writeChips(chips []Chip) {
	var parts []string
	for _, c := range chips {
		parts = append(parts, r.styleChip(c))
	}
	fmt.Fprintln(r.stdout, "  "+strings.Join(parts, "  "))
}

func (r *Renderer) styleChip(c Chip) string {
	if r.noColor {
		return "[" + c.Label + "]"
	}
	base := lipgloss.NewStyle().Padding(0, 1).Bold(true)
	switch c.Tone {
	case ToneActive:
		base = base.Background(lipgloss.Color("28")).Foreground(lipgloss.Color("15"))
	case ToneArchived:
		base = base.Background(lipgloss.Color("240")).Foreground(lipgloss.Color("15"))
	case ToneExpired:
		base = base.Background(lipgloss.Color("124")).Foreground(lipgloss.Color("15"))
	case ToneTrial:
		base = base.Background(lipgloss.Color("33")).Foreground(lipgloss.Color("15"))
	case ToneWarning:
		base = base.Background(lipgloss.Color("172")).Foreground(lipgloss.Color("15"))
	default:
		base = base.Background(lipgloss.Color("236")).Foreground(lipgloss.Color("252"))
	}
	return base.Render(c.Label)
}

func (r *Renderer) writeCardTable(t CardTable) {
	widths := make([]int, len(t.Columns))
	for i, c := range t.Columns {
		widths[i] = len(c)
	}
	for _, row := range t.Rows {
		for i, cell := range row {
			if i >= len(widths) {
				continue
			}
			if l := len(cell); l > widths[i] {
				widths[i] = l
			}
		}
	}
	headerStyle := lipgloss.NewStyle().Bold(true)
	fmt.Fprint(r.stdout, "  ")
	for i, c := range t.Columns {
		if i > 0 {
			fmt.Fprint(r.stdout, "  ")
		}
		fmt.Fprint(r.stdout, r.style(headerStyle, padRight(c, widths[i])))
	}
	fmt.Fprintln(r.stdout)
	for _, row := range t.Rows {
		fmt.Fprint(r.stdout, "  ")
		for i, cell := range row {
			if i > 0 {
				fmt.Fprint(r.stdout, "  ")
			}
			fmt.Fprint(r.stdout, padRight(cell, widths[i]))
		}
		fmt.Fprintln(r.stdout)
	}
}

func (r *Renderer) writeLines(lines []CardLine) {
	keyWidth := 0
	for _, l := range lines {
		if len(l.Key) > keyWidth {
			keyWidth = len(l.Key)
		}
	}
	keyStyle := lipgloss.NewStyle().Faint(true)
	for _, l := range lines {
		fmt.Fprintf(r.stdout, "  %s  %s\n", r.style(keyStyle, padRight(l.Key+":", keyWidth+1)), l.Value)
	}
}
