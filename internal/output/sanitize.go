package output

import "strings"

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

func isControlRune(r rune) bool {
	if r == '\n' || r == '\t' {
		return false
	}
	return r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0x9F)
}
