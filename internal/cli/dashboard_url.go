package cli

import (
	"fmt"
	"strings"
)

// dashboardURL builds an app.revenuecat.com project URL from path segments,
// percent-encoding each one.
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

// not url.PathEscape: it leaves sub-delims like `&` unencoded
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
