package cli

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/output"
)

type recordingSkillsInstaller struct {
	args  []string
	calls int
}

func (r *recordingSkillsInstaller) Run(_ *cobra.Command, args []string) error {
	r.calls++
	r.args = append([]string(nil), args...)
	return nil
}

func TestSkillsInstallDelegatesToOfficialToolkit(t *testing.T) {
	installer := &recordingSkillsInstaller{}
	cmd := newSkillsCmdWithInstaller(installer)
	var stdout, stderr bytes.Buffer
	globals := &Globals{JSON: true, NoInput: true, AssumeYes: true}
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"install", "--agent", "codex", "--skill", "create-revenuecat-project"})
	cmd.SetContext(WithRuntime(context.Background(), &Runtime{
		Globals: globals,
		Out:     output.NewRenderer(&stdout, &stderr, true, true, false, ""),
	}))
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"skills", "add", officialToolkitSource, "--global", "--agent", "codex", "--skill", "create-revenuecat-project", "--yes"} {
		if !slices.Contains(installer.args, want) {
			t.Errorf("installer args missing %q: %v", want, installer.args)
		}
	}
	if installer.calls != 1 {
		t.Fatalf("installer calls = %d, want 1", installer.calls)
	}
}

func TestSkillsInstallProjectScope(t *testing.T) {
	installer := &recordingSkillsInstaller{}
	cmd := newSkillsCmdWithInstaller(installer)
	var stdout, stderr bytes.Buffer
	globals := &Globals{JSON: true, NoInput: true, AssumeYes: true}
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"install", "--project"})
	cmd.SetContext(WithRuntime(context.Background(), &Runtime{
		Globals: globals,
		Out:     output.NewRenderer(&stdout, &stderr, true, true, false, ""),
	}))
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(installer.args, "--global") {
		t.Fatalf("project install args unexpectedly contain --global: %v", installer.args)
	}
}

func TestSkillsInstallNoInputRequiresYes(t *testing.T) {
	installer := &recordingSkillsInstaller{}
	cmd := newSkillsCmdWithInstaller(installer)
	globals := &Globals{NoInput: true}
	cmd.SetContext(WithRuntime(context.Background(), &Runtime{
		Globals: globals,
		Out:     output.NewRenderer(cmd.OutOrStdout(), cmd.ErrOrStderr(), false, true, false, ""),
	}))
	cmd.SetArgs([]string{"install"})
	err := cmd.Execute()
	if err == nil || installer.calls != 0 {
		t.Fatalf("error = %v, installer calls = %d", err, installer.calls)
	}
}
