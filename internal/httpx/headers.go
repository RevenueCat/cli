// Package httpx holds small HTTP helpers shared across the CLI's API clients.
package httpx

import (
	"net/http"
	"strings"
)

// ParseHeaders parses newline-separated "Name: Value" pairs into an http.Header,
// skipping blank or colon-less lines. Returns nil when none are found.
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

// Apply sets each header from h on req, overriding existing values.
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
