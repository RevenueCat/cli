package output

import (
	"strings"
	"unicode"
)

// Sanitize strips control characters (C0 except \n and \t, DEL, and C1) from a
// value before it is rendered as human output. API values are arbitrary text
// and must render as visible characters only — they must never be able to move
// the cursor or address the terminal.
func Sanitize(s string) string {
	if !strings.ContainsFunc(s, isControlRune) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !isControlRune(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// unicode.IsControl is exactly the Cc category: C0, DEL, and C1.
func isControlRune(r rune) bool {
	return unicode.IsControl(r) && r != '\n' && r != '\t'
}

// SanitizeLine is Sanitize for single-line contexts — table cells, labels,
// breadcrumbs, chips — where a newline would fake extra rows and a tab would
// shift columns; both collapse to a space.
func SanitizeLine(s string) string {
	s = Sanitize(s)
	if !strings.ContainsAny(s, "\n\t") {
		return s
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		return r
	}, s)
}
