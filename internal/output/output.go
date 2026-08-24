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
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/itchyny/gojq"
	"github.com/muesli/termenv"
)

// ErrBadFormat signals an unparseable --format expression. CLI maps this to
// exit 2 (usage error) so agents can distinguish bad input from runtime
// failures.
var ErrBadFormat = errors.New("invalid --format expression")

type Renderer struct {
	stdout  io.Writer
	stderr  io.Writer
	json    bool
	noColor bool
	quiet   bool
	format  string // jq expression applied to --json output (via gojq)

	success lipgloss.Style
	info    lipgloss.Style
	warn    lipgloss.Style
	errSty  lipgloss.Style
	dim     lipgloss.Style
	accent  lipgloss.Style
}

func NewRenderer(stdout, stderr io.Writer, jsonMode, noColor, quiet bool, format string) *Renderer {
	noColor = noColor || os.Getenv("NO_COLOR") != ""
	if noColor {
		// Make --no-color reach direct lipgloss usage too (guided rail, prompts,
		// ledger), not just this Renderer's own styling. termenv already honors
		// the NO_COLOR env var; this covers the flag. Not restored — it's a global
		// for the lifetime of a one-shot CLI process that's about to exit.
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	r := &Renderer{
		stdout:  stdout,
		stderr:  stderr,
		json:    jsonMode,
		noColor: noColor,
		quiet:   quiet,
		format:  format,
	}
	if !noColor {
		r.success = StyleSuccess
		r.info = StyleDim
		r.warn = StyleWarn
		r.errSty = StyleError
		r.dim = StyleDim
		r.accent = StyleAccent
	}
	return r
}

func (r *Renderer) IsJSON() bool { return r.json }

// NoColor reports whether all ANSI output is disabled (--no-color or NO_COLOR),
// so callers composing their own escapes (e.g. OSC 8 hyperlinks) can skip them.
func (r *Renderer) NoColor() bool { return r.noColor }

// Tone is a semantic style for callers that compose their own output.
type Tone int

const (
	ToneAccent  Tone = iota // brand landmark (bar, identity)
	ToneTitle               // bold section label
	ToneDim                 // secondary/description text
	ToneSuccess             // affirmative (logged in)
	ToneCommand             // a command the user can type
	ToneLink                // a URL to open
)

// Paint renders text in the given tone, unstyled when color is disabled.
func (r *Renderer) Paint(t Tone, text string) string {
	var s lipgloss.Style
	switch t {
	case ToneAccent:
		s = StyleAccent
	case ToneTitle:
		s = StyleTitle
	case ToneSuccess:
		s = StyleSuccess
	case ToneCommand:
		s = StyleCommand
	case ToneLink:
		s = StyleInfo
	default:
		s = StyleDim
	}
	return r.style(s, text)
}

// Panel wraps pre-styled lines in a rounded border box sized to fit its
// content. The frame is drawn uncolored when color is disabled.
func (r *Renderer) Panel(lines ...string) string {
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	if !r.noColor {
		box = box.BorderForeground(NeutralGray)
	}
	return box.Render(strings.Join(lines, "\n"))
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
// agents can rely on shape. When --format is set alongside --json, the
// envelope is fed through gojq; each output value is emitted on its own line.
func (r *Renderer) Render(v any) error {
	if r.json {
		env := map[string]any{
			"data":           v,
			"schema_version": 1,
		}
		if r.format != "" {
			return r.renderJSONFiltered(env)
		}
		enc := json.NewEncoder(r.stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(env)
	}
	if r.format != "" {
		// --format without --json: warn on stderr, fall through to pretty.
		fmt.Fprintln(r.stderr, r.style(r.warn, "! ")+"--format is only applied to --json output; ignoring")
	}
	return r.renderHuman(v)
}

// RenderJSON always emits JSON, even in human mode — for commands whose
// output IS structured data (rc schema, rc commands, SDK payload dumps).
func (r *Renderer) RenderJSON(v any) error {
	if r.json {
		return r.Render(v)
	}
	enc := json.NewEncoder(r.stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// renderHuman is the human-mode fallback for structured results: aligned
// key/value lines instead of raw JSON. Commands with richer views use
// RenderCard/RenderTable; this guarantees no human ever sees a JSON blob.
func (r *Renderer) renderHuman(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		// Not an object (array/scalar): print compactly.
		fmt.Fprintln(r.stdout, humanValue(raw))
		return nil
	}
	keys := humanKeyOrder(m)
	width := 0
	for _, k := range keys {
		if len(k) > width {
			width = len(k)
		}
	}
	for _, k := range keys {
		fmt.Fprintf(r.stdout, "%s  %s\n", r.style(r.dim, padRight(k, width)), humanFieldValue(k, m[k]))
	}
	return nil
}

// humanFieldValue formats millisecond-epoch timestamp fields as dates.
func humanFieldValue(key string, raw json.RawMessage) string {
	if strings.HasSuffix(key, "_at") {
		var ms int64
		if json.Unmarshal(raw, &ms) == nil && ms > 1_000_000_000_000 && ms < 10_000_000_000_000 {
			return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04 MST")
		}
	}
	return humanValue(raw)
}

// humanKeyOrder puts identity first, drops noise keys, sorts the rest.
func humanKeyOrder(m map[string]json.RawMessage) []string {
	first := []string{"id", "name", "lookup_key", "display_name"}
	drop := map[string]bool{"object": true, "ok": true}
	var keys []string
	seen := map[string]bool{}
	for _, k := range first {
		if _, exists := m[k]; exists {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	var rest []string
	for k := range m {
		if !seen[k] && !drop[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	return append(keys, rest...)
}

// humanValue renders one JSON value on one line: scalars verbatim, short
// composites as compact JSON, long ones summarized.
func humanValue(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if s == "" {
			return "—"
		}
		return s
	}
	trimmed := string(raw)
	if trimmed == "null" {
		return "—"
	}
	if len(trimmed) <= 60 {
		return trimmed
	}
	if trimmed[0] == '[' {
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) == nil {
			return fmt.Sprintf("(%d items)", len(items))
		}
	}
	return "{…}"
}

// renderJSONFiltered runs the user's gojq expression over the envelope and
// emits each result as a separate JSON-encoded line (NDJSON-ish). Strings
// come out unquoted so `--format '.data.items[].id'` produces shell-friendly
// output rather than `"id1"\n"id2"`.
//
// gojq works on plain JSON-decoded values (map[string]any / []any / scalars),
// not on Go structs. We marshal the envelope and unmarshal back to `any` so
// expressions like `.data.items[].id` work regardless of how typed the
// underlying payload is.
func (r *Renderer) renderJSONFiltered(env any) error {
	q, err := gojq.Parse(r.format)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBadFormat, err)
	}
	jsonBytes, err := json.Marshal(env)
	if err != nil {
		return err
	}
	var input any
	if err := json.Unmarshal(jsonBytes, &input); err != nil {
		return err
	}
	iter := q.Run(input)
	for {
		v, ok := iter.Next()
		if !ok {
			return nil
		}
		if e, isErr := v.(error); isErr {
			return fmt.Errorf("%w: %v", ErrBadFormat, e)
		}
		switch t := v.(type) {
		case string:
			fmt.Fprintln(r.stdout, t)
		case nil:
			// jq emits nil for `.missing`; skip rather than print "null".
		default:
			b, err := json.Marshal(t)
			if err != nil {
				return err
			}
			fmt.Fprintln(r.stdout, string(b))
		}
	}
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
	if r.json || r.quiet {
		return
	}
	fmt.Fprintln(r.stderr, r.style(r.success, "✓ ")+msg)
}

func (r *Renderer) Info(msg string) {
	if r.json || r.quiet {
		return
	}
	fmt.Fprintln(r.stderr, r.style(r.info, "· ")+msg)
}

// Hyperlink wraps styledLabel in an OSC 8 terminal hyperlink pointing at url.
// Supporting terminals make it clickable; others render the label text. This is
// the one place the OSC 8 escape lives.
func Hyperlink(styledLabel, url string) string {
	return "\x1b]8;;" + url + "\x1b\\" + styledLabel + "\x1b]8;;\x1b\\"
}

// LinkText renders a clickable hyperlink (OSC 8) with a custom label instead of
// the raw URL, so long auth URLs don't dominate the output. With color off it
// falls back to "label (url)" so the URL stays copyable.
func (r *Renderer) LinkText(label, url string) string {
	if r.noColor {
		return label + " (" + url + ")"
	}
	return Hyperlink(lipgloss.NewStyle().Foreground(BrandRed).Underline(true).Render(label), url)
}

// Link renders a URL as a clickable, underlined brand-accent hyperlink. Falls
// back to the plain URL when color is off (our proxy for a dumb/non-interactive
// terminal), so nothing leaks escape codes into piped or --no-color output.
func (r *Renderer) Link(url string) string {
	if r.noColor {
		return url
	}
	return Hyperlink(lipgloss.NewStyle().Foreground(BrandRed).Underline(true).Render(url), url)
}

// LinkLine prints a single indented, clickable link on its own line (stderr
// chatter), for "open this URL" moments in guided flows.
func (r *Renderer) LinkLine(url string) {
	if r.json || r.quiet {
		return
	}
	fmt.Fprintln(r.stderr, "  "+r.Link(url))
}

// Hint is guidance about what to do next — a whole line, dimmed, so it never
// competes with results.
func (r *Renderer) Hint(msg string) {
	if r.json || r.quiet {
		return
	}
	fmt.Fprintln(r.stderr, r.style(r.dim, "  "+msg))
}

// Title starts a visually distinct section: a brand-colored bar plus a bold
// heading, preceded by a blank line so output breathes.
func (r *Renderer) Title(msg string) {
	if r.json || r.quiet {
		return
	}
	fmt.Fprintln(r.stderr)
	fmt.Fprintln(r.stderr, r.style(r.accent, "▍ ")+r.style(StyleTitle, msg))
}

// Lead is the orienting sentence(s) under a Title: what this flow is for
// and what the pieces mean, in plain language. Normal weight — education
// is content, not chrome — and wrapped for readability. One Lead per flow,
// at the top; stay terse afterwards.
func (r *Renderer) Lead(text string) {
	if r.json || r.quiet {
		return
	}
	const width = 76
	words := strings.Fields(text)
	line := " "
	for _, w := range words {
		if len(line)+1+len(w) > width {
			fmt.Fprintln(r.stderr, line)
			line = " "
		}
		line += " " + w
	}
	if strings.TrimSpace(line) != "" {
		fmt.Fprintln(r.stderr, line)
	}
	fmt.Fprintln(r.stderr)
}

// Notice renders a must-read callout: a colored bar block that stands out
// from the narration without being an error. Use for trust and safety
// statements at the moment they matter (e.g. before a credential prompt) —
// never dim these.
func (r *Renderer) Notice(lines ...string) {
	if r.json || r.quiet {
		return
	}
	fmt.Fprintln(r.stderr)
	bar := lipgloss.NewStyle().Foreground(InfoBlue).Bold(true)
	for _, line := range lines {
		fmt.Fprintln(r.stderr, r.style(bar, "▐ ")+line)
	}
	fmt.Fprintln(r.stderr)
}

// Answer is the durable receipt for an answered prompt. Interactive forms
// erase themselves when they complete, so every decision the user makes is
// echoed back as a permanent transcript line.
func (r *Renderer) Answer(key, value string) {
	if r.json || r.quiet {
		return
	}
	fmt.Fprintf(r.stderr, "%s %s %s\n", r.style(r.success, "✓"), r.style(r.dim, padRight(key, 26)), value)
}

// Plan renders the guided-command plan: a titled, numbered list of the
// steps a consequential flow is about to take. Keep steps terse and
// state-aware — list what will happen this run, not every possibility.
func (r *Renderer) Plan(steps []string) {
	if r.json || r.quiet {
		return
	}
	r.Title("Plan")
	for i, step := range steps {
		r.Info(fmt.Sprintf("%d. %s", i+1, step))
	}
}

// Field prints an aligned key/value line for status blocks: dim key,
// normal value, indented under a Title.
func (r *Renderer) Field(key, value string, note ...string) {
	if r.json || r.quiet {
		return
	}
	if len(note) > 0 && note[0] != "" {
		// Pad the value only when a note follows so notes column-align and
		// bare values carry no trailing whitespace.
		value = padRight(value, 15) + "  " + r.style(r.dim, "· "+note[0])
	}
	fmt.Fprintf(r.stderr, "  %s  %s\n", r.style(r.dim, padRight(key, 26)), value)
}

// Blank prints an empty separator line between logical sections.
func (r *Renderer) Blank() {
	if r.json || r.quiet {
		return
	}
	fmt.Fprintln(r.stderr)
}

func (r *Renderer) Warn(msg string) {
	if r.json || r.quiet {
		return
	}
	fmt.Fprintln(r.stderr, r.style(r.warn, "! ")+msg)
}

// AlwaysWarn writes a warning to stderr even in --json mode.
func (r *Renderer) AlwaysWarn(msg string) {
	if r.quiet {
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
