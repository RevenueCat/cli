// Package output renders command results in either pretty TTY form or stable
// machine-readable JSON. The renderer is created once per command in
// PersistentPreRunE and passed via the Runtime.
//
// Contract:
//   - stdout is data.
//   - stderr is chatter (Success / Info / Hint messages, spinners, errors).
//   - --json never auto-activates from pipe detection; the user opts in.
//   - --no-color (or NO_COLOR env) disables ALL ANSI output, including
//     text-style attributes like bold (not just color).
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
)

type Renderer struct {
	stdout  io.Writer
	stderr  io.Writer
	json    bool
	noColor bool
	format  string // jsonpath-style projection (TODO: wire to a jq-lite eval)

	success lipgloss.Style
	info    lipgloss.Style
	warn    lipgloss.Style
	errSty  lipgloss.Style
}

func NewRenderer(stdout, stderr io.Writer, jsonMode, noColor bool, format string) *Renderer {
	noColor = noColor || os.Getenv("NO_COLOR") != ""
	r := &Renderer{
		stdout:  stdout,
		stderr:  stderr,
		json:    jsonMode,
		noColor: noColor,
		format:  format,
	}
	if !noColor {
		r.success = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
		r.info = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
		r.warn = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
		r.errSty = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	}
	return r
}

// style returns either the styled rendering of s, or s unmodified when colors
// are disabled. This is the single place we make that choice — every styled
// write goes through here.
func (r *Renderer) style(s lipgloss.Style, text string) string {
	if r.noColor {
		return text
	}
	return s.Render(text)
}

// Render writes the primary result. JSON mode emits a stable envelope so
// agents can rely on shape.
func (r *Renderer) Render(v any) error {
	if r.json {
		env := map[string]any{
			"data":           v,
			"schema_version": 1,
		}
		enc := json.NewEncoder(r.stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(env)
	}
	enc := json.NewEncoder(r.stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Table is a column-oriented view rendered as a simple aligned table in TTY
// mode and as the raw underlying value in --json mode. Header order is the
// caller's responsibility — map iteration would be unstable.
type Table struct {
	Columns []string
	Rows    [][]string
	// Raw is the structured value returned under --json. Required so JSON
	// output stays useful (a table of strings would lose typing).
	Raw any
}

func (r *Renderer) RenderTable(t Table) error {
	if r.json {
		return r.Render(t.Raw)
	}
	if len(t.Rows) == 0 {
		fmt.Fprintln(r.stderr, r.style(r.info, "• ")+"no results")
		return nil
	}
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
	for i, c := range t.Columns {
		if i > 0 {
			fmt.Fprint(r.stdout, "  ")
		}
		fmt.Fprint(r.stdout, r.style(headerStyle, padRight(c, widths[i])))
	}
	fmt.Fprintln(r.stdout)
	for _, row := range t.Rows {
		for i, cell := range row {
			if i > 0 {
				fmt.Fprint(r.stdout, "  ")
			}
			fmt.Fprint(r.stdout, padRight(cell, widths[i]))
		}
		fmt.Fprintln(r.stdout)
	}
	return nil
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + spaces(n-len(s))
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}

func (r *Renderer) Success(msg string) {
	if r.json {
		return
	}
	fmt.Fprintln(r.stderr, r.style(r.success, "✓ ")+msg)
}

func (r *Renderer) Info(msg string) {
	if r.json {
		return
	}
	fmt.Fprintln(r.stderr, r.style(r.info, "• ")+msg)
}

func (r *Renderer) Warn(msg string) {
	if r.json {
		return
	}
	fmt.Fprintln(r.stderr, r.style(r.warn, "! ")+msg)
}

func (r *Renderer) Error(msg string) {
	if r.json {
		return
	}
	fmt.Fprintln(r.stderr, r.style(r.errSty, "✗ ")+msg)
}
