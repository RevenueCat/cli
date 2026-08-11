package cli

import "testing"

func TestDashboardProjectID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"hash starting with hex letter is preserved", "projf61a7d28", "f61a7d28"},
		{"hash starting with digit", "proj0abc", "0abc"},
		{"hash starting with non-hex letter", "projzed123", "zed123"},
		{"existing example", "proj5adb8697", "5adb8697"},
		{"underscore separator form", "proj_abc", "abc"},
		{"id without proj prefix returned unchanged", "5adb8697", "5adb8697"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dashboardProjectID(tt.in); got != tt.want {
				t.Errorf("dashboardProjectID(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
