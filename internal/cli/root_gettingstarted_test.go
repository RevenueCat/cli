package cli_test

import (
	"strings"
	"testing"
)

func TestBareRC_LoggedOut_ShowsSetupSteps(t *testing.T) {
	out, _, err := runCmd(t, "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"New here?", "rc auth login", "rc projects use", "rc paywalls", "rc --help"} {
		if !strings.Contains(out, want) {
			t.Fatalf("logged-out home missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Logged in") {
		t.Fatalf("logged-out home should not claim a session:\n%s", out)
	}
	if strings.Contains(out, "Available Commands") {
		t.Fatalf("bare rc should not dump the full command list:\n%s", out)
	}

	// --help still lists the full command surface.
	help, _, err := runCmd(t, "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(help, "Available Commands") {
		t.Fatalf("rc --help should list commands:\n%s", help)
	}
}

func TestBareRC_LoggedInNoProject_NudgesProject(t *testing.T) {
	out, _, err := runCmd(t, "--no-color", "--api-key", "sk_test")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Logged in", "pick a project", "rc projects use", "rc paywalls"} {
		if !strings.Contains(out, want) {
			t.Fatalf("logged-in/no-project home missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "New here?") {
		t.Fatalf("authenticated home should not show first-run steps:\n%s", out)
	}
}

func TestBareRC_LoggedInWithProject_ShowsThingsToDo(t *testing.T) {
	out, _, err := runCmd(t, "--no-color", "--api-key", "sk_test", "--project-id", "proj_abc")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Logged in", "project proj_abc", "Do something", "rc paywalls"} {
		if !strings.Contains(out, want) {
			t.Fatalf("logged-in/project home missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "New here?") || strings.Contains(out, "pick a project") {
		t.Fatalf("home with an active project should not nudge setup:\n%s", out)
	}
}
