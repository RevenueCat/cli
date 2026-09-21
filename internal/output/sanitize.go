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
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
}

// SanitizeLine is Sanitize for single-line contexts — table cells, labels,
// breadcrumbs, chips — where a newline would fake extra rows and a tab would
// shift columns; both collapse to a space.
func SanitizeLine(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, s)
}
