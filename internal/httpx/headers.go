// Package httpx holds small HTTP helpers shared across the CLI's API clients.
package httpx

import (
	"net/http"
	"strings"
)

// ParseHeaders reads newline-separated "Name: Value" pairs — e.g. from the
// RC_HEADERS env var — into an http.Header. Blank lines and lines without a
// colon are skipped; names and values are trimmed. Later duplicates win.
//
// This lets an operator send arbitrary headers on every request without the
// header name being baked into the binary (RC_HEADERS=$'X-Some-Header: value').
func ParseHeaders(s string) http.Header {
	h := http.Header{}
	for _, line := range strings.Split(s, "\n") {
		name, value, ok := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			continue
		}
		h.Set(name, strings.TrimSpace(value))
	}
	if len(h) == 0 {
		return nil
	}
	return h
}

// Apply sets every header in h onto req, overriding any existing value. Callers
// invoke it after setting the standard headers so operator-supplied headers take
// precedence.
func Apply(req *http.Request, h http.Header) {
	for name, values := range h {
		for i, v := range values {
			if i == 0 {
				req.Header.Set(name, v)
			} else {
				req.Header.Add(name, v)
			}
		}
	}
}
