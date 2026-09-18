package tui

import "testing"

// OpenURL must refuse anything that is not http(s) before reaching any
// platform opener.
func TestOpenURL_RefusesNonHTTP(t *testing.T) {
	for _, u := range []string{
		"file:///etc/passwd",
		"-leading-dash",
		"javascript:alert(1)",
		"not a url",
		"",
	} {
		if err := OpenURL(u); err == nil {
			t.Errorf("OpenURL(%q) should refuse non-http(s) input", u)
		}
	}
}
