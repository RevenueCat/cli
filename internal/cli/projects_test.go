package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/output"
)

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

func TestWarnDirBindingShadow(t *testing.T) {
	writeBinding := func(t *testing.T, id string) {
		t.Helper()
		dir := t.TempDir()
		body := []byte(`{"project_id": "` + id + `"}`)
		if err := os.WriteFile(filepath.Join(dir, config.ProjectFileName), body, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)
	}

	run := func(t *testing.T, selected string) string {
		t.Helper()
		var errb bytes.Buffer
		rt := &Runtime{
			Globals: &Globals{},
			Out:     output.NewRenderer(&bytes.Buffer{}, &errb, false, true, false, ""),
		}
		warnDirBindingShadow(rt, selected)
		return errb.String()
	}

	t.Run("warns when binding shadows a different selected project", func(t *testing.T) {
		writeBinding(t, "proj_bound")
		out := run(t, "proj_chosen")
		if !strings.Contains(out, "proj_bound") || !strings.Contains(out, config.ProjectFileName) {
			t.Errorf("expected warning naming the binding project and file, got %q", out)
		}
	})

	t.Run("silent when binding matches the selected project", func(t *testing.T) {
		writeBinding(t, "proj_bound")
		if out := run(t, "proj_bound"); out != "" {
			t.Errorf("expected no warning when binding matches, got %q", out)
		}
	})

	t.Run("warns when the default was cleared and a binding still applies", func(t *testing.T) {
		writeBinding(t, "proj_bound")
		if out := run(t, ""); !strings.Contains(out, "proj_bound") {
			t.Errorf("expected warning after clearing the default, got %q", out)
		}
	})

	t.Run("silent when there is no binding", func(t *testing.T) {
		t.Chdir(t.TempDir())
		if out := run(t, "proj_chosen"); out != "" {
			t.Errorf("expected no warning without a binding, got %q", out)
		}
	})
}
