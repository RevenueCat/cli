package cli

import "testing"

func TestFormatISODuration(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"P0D", "none"},
		{"PT0S", "none"},
		{"P1D", "1 day"},
		{"P3D", "3 days"},
		{"P7D", "1 week"},
		{"P1W", "1 week"},
		{"P1M", "1 month"},
		{"P2M", "2 months"},
		{"P1Y", "1 year"},
		{"P1Y1M", "1 year 1 month"},
		{"P14D", "2 weeks"},
		{"PT1H30M", "1 hour 30 minutes"},
		{"", ""},
		{"banana", "banana"},
		{"P", "P"},
		{"P1X", "P1X"},
	}
	for _, c := range cases {
		if got := formatISODuration(c.in); got != c.want {
			t.Errorf("formatISODuration(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
