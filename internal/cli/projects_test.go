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

	run := func(t *testing.T, selected string) (string, *dirBindingShadow) {
		t.Helper()
		var errb bytes.Buffer
		rt := &Runtime{
			Globals: &Globals{},
			Out:     output.NewRenderer(&bytes.Buffer{}, &errb, false, true, false, ""),
		}
		shadow := warnDirBindingShadow(rt, selected)
		return errb.String(), shadow
	}

	t.Run("warns when binding shadows a different selected project", func(t *testing.T) {
		writeBinding(t, "proj_bound")
		out, shadow := run(t, "proj_chosen")
		if !strings.Contains(out, "proj_bound") || !strings.Contains(out, config.ProjectFileName) {
			t.Errorf("expected warning naming the binding project and file, got %q", out)
		}
		if shadow == nil {
			t.Fatal("expected a shadow to be returned for --json output")
		}
		if shadow.ProjectID != "proj_bound" {
			t.Errorf("shadow.ProjectID = %q, want proj_bound", shadow.ProjectID)
		}
		if !strings.Contains(shadow.File, config.ProjectFileName) {
			t.Errorf("shadow.File = %q, want it to name %s", shadow.File, config.ProjectFileName)
		}
	})

	t.Run("silent when binding matches the selected project", func(t *testing.T) {
		writeBinding(t, "proj_bound")
		out, shadow := run(t, "proj_bound")
		if out != "" {
			t.Errorf("expected no warning when binding matches, got %q", out)
		}
		if shadow != nil {
			t.Errorf("expected no shadow when binding matches, got %+v", shadow)
		}
	})

	t.Run("warns when the default was cleared and a binding still applies", func(t *testing.T) {
		writeBinding(t, "proj_bound")
		out, shadow := run(t, "")
		if !strings.Contains(out, "proj_bound") {
			t.Errorf("expected warning after clearing the default, got %q", out)
		}
		if shadow == nil || shadow.ProjectID != "proj_bound" {
			t.Errorf("expected shadow naming proj_bound after clearing, got %+v", shadow)
		}
	})

	t.Run("silent when there is no binding", func(t *testing.T) {
		t.Chdir(t.TempDir())
		out, shadow := run(t, "proj_chosen")
		if out != "" {
			t.Errorf("expected no warning without a binding, got %q", out)
		}
		if shadow != nil {
			t.Errorf("expected no shadow without a binding, got %+v", shadow)
		}
	})

	// Under --json the human Warn/Hint are no-ops, but the shadow must still be
	// returned so the rendered result can carry it — the mode agents run in.
	t.Run("json mode still returns the shadow", func(t *testing.T) {
		writeBinding(t, "proj_bound")
		var errb bytes.Buffer
		rt := &Runtime{
			Globals: &Globals{},
			Out:     output.NewRenderer(&bytes.Buffer{}, &errb, true, true, false, ""),
		}
		shadow := warnDirBindingShadow(rt, "proj_chosen")
		if errb.Len() != 0 {
			t.Errorf("json mode must not write human warnings to stderr, got %q", errb.String())
		}
		if shadow == nil || shadow.ProjectID != "proj_bound" {
			t.Errorf("expected shadow in json mode, got %+v", shadow)
		}
	})
}
