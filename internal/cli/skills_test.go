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

func TestSkillsShowsCopyReadyPromptsWithoutJSONDump(t *testing.T) {
	cmd := newSkillsCmdWithInstaller(&recordingSkillsInstaller{})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"prompts"})
	cmd.SetContext(WithRuntime(context.Background(), &Runtime{
		Globals: &Globals{},
		Out:     output.NewRenderer(&stdout, &stderr, false, true, false, ""),
	}))
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("human skills output dumped JSON: %s", stdout.String())
	}
	for _, want := range []string{"Copy one of these starter prompts", "Make this app Test Store-ready", "Connect my Apple account", "Sync my App Store catalog", "create-revenuecat-project skill", "integrate-revenuecat skill", "revenuecat-store-state skill", "revenuecat-status skill"} {
		if !bytes.Contains(stderr.Bytes(), []byte(want)) {
			t.Errorf("starter prompts missing %q: %s", want, stderr.String())
		}
	}
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
	for _, want := range []string{"Start a new agent session", "rc skills prompts"} {
		if !bytes.Contains(stdout.Bytes(), []byte(want)) {
			t.Errorf("install JSON missing %q: %s", want, stdout.String())
		}
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

func TestSkillsInstallUsesBranchFromEnvironment(t *testing.T) {
	t.Setenv("RC_SKILLS_BRANCH", "rc-cli-project-setup-workflows")
	installer := &recordingSkillsInstaller{}
	cmd := newSkillsCmdWithInstaller(installer)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"install"})
	cmd.SetContext(WithRuntime(context.Background(), &Runtime{
		Globals: &Globals{JSON: true},
		Out:     output.NewRenderer(&stdout, &stderr, true, true, false, ""),
	}))
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	wantSource := "https://github.com/RevenueCat/ai-toolkit/tree/rc-cli-project-setup-workflows"
	if !slices.Contains(installer.args, wantSource) {
		t.Fatalf("installer args = %v, want %q", installer.args, wantSource)
	}
	for _, want := range []string{`"branch": "rc-cli-project-setup-workflows"`, `"source": "` + wantSource + `"`} {
		if !bytes.Contains(stdout.Bytes(), []byte(want)) {
			t.Errorf("install JSON missing %q: %s", want, stdout.String())
		}
	}
}

func TestSkillsInstallBranchFlagOverridesEnvironment(t *testing.T) {
	t.Setenv("RC_SKILLS_BRANCH", "from-env")
	installer := &recordingSkillsInstaller{}
	cmd := newSkillsCmdWithInstaller(installer)
	cmd.SetArgs([]string{"install", "--branch", "from-flag"})
	cmd.SetContext(WithRuntime(context.Background(), &Runtime{
		Globals: &Globals{},
		Out:     output.NewRenderer(cmd.OutOrStdout(), cmd.ErrOrStderr(), false, true, false, ""),
	}))
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(installer.args, "https://github.com/RevenueCat/ai-toolkit/tree/from-flag") {
		t.Fatalf("installer args = %v", installer.args)
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
