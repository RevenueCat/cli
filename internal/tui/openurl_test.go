package tui

import "testing"

// OpenURL must refuse anything that is not http(s) before reaching any
// platform opener.
func TestOpenURL_RefusesNonHTTP(t *testing.T) {
	for _, u := range []string{
		"file:///tmp/notes.txt",
		"-leading-dash",
		"ftp://example.com/file",
		"not a url",
		"",
	} {
		if err := OpenURL(u); err == nil {
			t.Errorf("OpenURL(%q) should refuse non-http(s) input", u)
		}
	}
}
