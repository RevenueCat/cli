package cli

import (
	"fmt"
	"strings"
)

// dashboardURL builds an app.revenuecat.com project URL from path segments,
// strictly percent-encoding each one. Some segments (customer IDs) are
// arbitrary free text, and these URLs are displayed and handed to the OS URL
// opener, so every byte outside the RFC 3986 unreserved set is encoded —
// url.PathEscape is not enough, since it passes sub-delims like `&` through.
func dashboardURL(projectID string, segments ...string) string {
	var b strings.Builder
	b.WriteString("https://app.revenuecat.com/projects/")
	b.WriteString(escapePathSegment(dashboardProjectID(projectID)))
	for _, s := range segments {
		b.WriteByte('/')
		b.WriteString(escapePathSegment(s))
	}
	return b.String()
}

func escapePathSegment(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '.', c == '_', c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
