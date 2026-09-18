package cli

import (
	"strings"
	"testing"
)

// Some URL segments (customer IDs) are arbitrary free text. Everything outside
// the RFC 3986 unreserved set must be percent-encoded so the URL stays a
// single well-formed token everywhere it is displayed or opened.
func TestDashboardURL_EncodesSpecialCharacters(t *testing.T) {
	cases := map[string]string{
		"cus_plain-1.2_~ok":              "https://app.revenuecat.com/projects/abc123/customers/cus_plain-1.2_~ok",
		"amp&ersand>redirect&":           "https://app.revenuecat.com/projects/abc123/customers/amp%26ersand%3Eredirect%26",
		`pipe|and<redirect>and"quote"`:   "https://app.revenuecat.com/projects/abc123/customers/pipe%7Cand%3Credirect%3Eand%22quote%22",
		"with spaces and\ttabs":          "https://app.revenuecat.com/projects/abc123/customers/with%20spaces%20and%09tabs",
		"semi;colon^caret%percent$var":   "https://app.revenuecat.com/projects/abc123/customers/semi%3Bcolon%5Ecaret%25percent%24var",
		"slash/and?query#frag":           "https://app.revenuecat.com/projects/abc123/customers/slash%2Fand%3Fquery%23frag",
		"()parens'quote`backtick{brace}": "https://app.revenuecat.com/projects/abc123/customers/%28%29parens%27quote%60backtick%7Bbrace%7D",
	}
	for id, want := range cases {
		if got := dashboardURL("proj_abc123", "customers", id); got != want {
			t.Errorf("dashboardURL(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestDashboardURL_NoSegments(t *testing.T) {
	if got := dashboardURL("proj_abc123", "offerings"); got != "https://app.revenuecat.com/projects/abc123/offerings" {
		t.Errorf("got %q", got)
	}
}

// None of these characters may ever appear raw after the path root, no matter
// what future edits do to the encoder.
func TestDashboardURL_NoRawSpecialCharacters(t *testing.T) {
	special := "&|<>\"' ;^`$(){}"
	got := dashboardURL("proj_abc123", "customers", special)
	rest := strings.TrimPrefix(got, "https://app.revenuecat.com/projects/")
	if strings.ContainsAny(rest, special) {
		t.Errorf("raw special character survived encoding: %q", got)
	}
}
