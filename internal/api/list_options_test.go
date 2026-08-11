package api

import "testing"

func TestListOptionsQuery(t *testing.T) {
	cases := map[string]struct {
		opts *ListOptions
		want string
	}{
		"nil":         {nil, ""},
		"empty":       {&ListOptions{}, ""},
		"limit only":  {&ListOptions{Limit: 5}, "?limit=5"},
		"cursor only": {&ListOptions{StartingAfter: "x"}, "?starting_after=x"},
		"both":        {&ListOptions{Limit: 5, StartingAfter: "x"}, "?limit=5&starting_after=x"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := c.opts.query(); got != c.want {
				t.Fatalf("query() = %q, want %q", got, c.want)
			}
		})
	}
}
