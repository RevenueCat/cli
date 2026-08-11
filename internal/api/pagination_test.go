package api_test

import (
	"testing"

	"github.com/revenuecat/cli/internal/api"
)

func TestPageNextCursor(t *testing.T) {
	cases := map[string]struct {
		nextPage string
		want     string
	}{
		"parses starting_after": {"https://api.revenuecat.com/v2/projects/p/fonts?starting_after=font_abc&limit=2", "font_abc"},
		"relative url":          {"/v2/projects/p/media_assets?starting_after=medas_x", "medas_x"},
		"no next page":          {"", ""},
		"no cursor param":       {"https://api.revenuecat.com/v2/projects/p/fonts", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := api.Page[int]{NextPage: c.nextPage}.NextCursor()
			if got != c.want {
				t.Fatalf("NextCursor(%q) = %q, want %q", c.nextPage, got, c.want)
			}
		})
	}
}
