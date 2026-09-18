package output

import (
	"fmt"
	"strings"
)

// Sanitize renders terminal control sequences inert. Everything the CLI shows
// a human passes through here first, because most of what it shows came from
// somewhere else: API responses, store metadata, App User IDs a customer chose
// for themselves. A terminal *acts* on those bytes rather than printing them —
// OSC 52 rewrites the clipboard, CSI moves the cursor, CR overwrites the line
// just printed — so an identifier is enough to drive the reader's terminal.
// After this, remote text can only ever be shown.
//
// Newline and tab survive: they move the cursor the same way ordinary text
// does, and some payloads legitimately carry them. Every other C0/C1 control
// and DEL becomes its escaped literal (`\x1b`), which is also what a human
// needs to see to understand what the value actually contains.
//
// Sanitize is not applied to strings the CLI styled itself (Paint, Panel,
// Link) — those carry deliberate escapes and are built from our own literals.
func Sanitize(s string) string {
	if !strings.ContainsFunc(s, isControl) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch {
		case !isControl(r):
			b.WriteRune(r)
		case r > 0x7f:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\x%02x`, r)
		}
	}
	return b.String()
}

// sanitizeAll is the slice form, for table rows and other cell collections.
func sanitizeAll(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = Sanitize(v)
	}
	return out
}

// isControl reports whether r drives the terminal instead of printing.
// C1 (0x80–0x9f) counts: terminals in 8-bit mode read 0x9b as CSI.
func isControl(r rune) bool {
	if r == '\n' || r == '\t' {
		return false
	}
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}
