package output

import (
	"strings"
	"unicode"
)

// Sanitize strips control characters (C0 except \n and \t, DEL, and C1), the
// bidirectional reordering controls, and the line/paragraph separators from a
// value before it is rendered as human output. API values are arbitrary text
// and must render as visible characters only — they must never be able to move
// the cursor, address the terminal, or reorder the glyphs around them.
func Sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) || isBidiControl(r) || unicode.In(r, unicode.Zl, unicode.Zp) {
			return -1
		}
		return r
	}, s)
}

// SanitizeLine is Sanitize for single-line contexts — table cells, labels,
// breadcrumbs, chips — where a newline would fake extra rows and a tab would
// shift columns; those and the Unicode line/paragraph separators collapse to a
// space.
func SanitizeLine(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t' || unicode.In(r, unicode.Zl, unicode.Zp):
			return ' '
		case unicode.IsControl(r) || isBidiControl(r):
			return -1
		}
		return r
	}, s)
}

// isBidiControl reports whether r is a Unicode bidirectional embedding,
// override or isolate control. They are invisible but reorder the glyphs around
// them, so a value carrying one renders as text that reads differently from the
// bytes it came from — the "Trojan Source" spoofing class (CVE-2021-42574).
// unicode.IsControl covers only Cc, which none of these are.
//
// Deliberately narrow: the rest of category Cf (zero-width joiner and
// non-joiner, variation selectors, soft hyphen) is load-bearing for emoji
// sequences and for Arabic and Indic shaping, so stripping it would corrupt
// legitimate names rather than protect anyone.
func isBidiControl(r rune) bool {
	switch {
	case r == '\u061C': // ARABIC LETTER MARK
		return true
	case r >= '\u202A' && r <= '\u202E': // LRE, RLE, PDF, LRO, RLO
		return true
	case r >= '\u2066' && r <= '\u2069': // LRI, RLI, FSI, PDI
		return true
	}
	return false
}
