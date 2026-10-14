package output

import (
	"fmt"
	"strings"
	"unicode"
)

// Sanitize removes terminal controls, bidi controls, and Unicode line/paragraph
// separators before styling human output. Other format characters are escaped
// visibly, except join controls needed for emoji sequences and script shaping.
// Newlines and tabs remain available for multiline output.
func Sanitize(s string) string {
	return sanitize(s, false)
}

// SanitizeLine also collapses newlines, tabs, and Unicode line/paragraph
// separators to spaces so untrusted values cannot fake rows or shift columns.
func SanitizeLine(s string) string {
	return sanitize(s, true)
}

func sanitize(s string, singleLine bool) string {
	var out strings.Builder
	out.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\t' || unicode.In(r, unicode.Zl, unicode.Zp):
			if singleLine {
				out.WriteByte(' ')
			} else if r == '\n' || r == '\t' {
				out.WriteRune(r)
			}
		case unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r):
			continue
		case unicode.Is(unicode.Cf, r) && !unicode.Is(unicode.Join_Control, r):
			if r <= 0xffff {
				fmt.Fprintf(&out, `\u%04x`, r)
			} else {
				fmt.Fprintf(&out, `\U%08x`, r)
			}
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}
