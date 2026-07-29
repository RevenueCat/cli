package tui

import "testing"

func TestAbsolutizeLinks(t *testing.T) {
	const base = "https://app.revenuecat.com"
	cases := []struct{ in, want string }{
		{
			"see [overview](/projects/abc/overview) for details",
			"see [overview](https://app.revenuecat.com/projects/abc/overview) for details",
		},
		{
			"go to /projects/abc/paywalls to edit it.",
			"go to https://app.revenuecat.com/projects/abc/paywalls to edit it.",
		},
		{
			"(check /settings/ai first)",
			"(check https://app.revenuecat.com/settings/ai first)",
		},
		{
			"path `/projects/abc` in backticks",
			"path `https://app.revenuecat.com/projects/abc` in backticks",
		},
		{
			"already absolute https://app.revenuecat.com/projects/abc stays",
			"already absolute https://app.revenuecat.com/projects/abc stays",
		},
		{
			"unrelated /usr/local/bin/rc path untouched",
			"unrelated /usr/local/bin/rc path untouched",
		},
		{
			"/projects/abc at start of message",
			"https://app.revenuecat.com/projects/abc at start of message",
		},
	}
	for _, c := range cases {
		if got := absolutizeLinks(c.in, base); got != c.want {
			t.Errorf("absolutizeLinks(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}
