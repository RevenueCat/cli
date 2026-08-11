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
	for _, want := range []string{"rc · RevenueCat", "not logged in", "New here?", "rc auth login", "rc projects use"} {
		if !strings.Contains(out, want) {
			t.Fatalf("logged-out home missing %q:\n%s", want, out)
		}
	}
	// logged out shows neither the command map nor the full cobra dump
	if strings.Contains(out, "Build") || strings.Contains(out, "Available Commands") {
		t.Fatalf("logged-out home should stay lean:\n%s", out)
	}

	// --help still lists the full command surface (grouped into sections).
	help, _, err := runCmd(t, "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"customer", "paywalls", "webhooks"} {
		if !strings.Contains(help, want) {
			t.Fatalf("rc --help should list command %q:\n%s", want, help)
		}
	}
}

func TestBareRC_AllShowsFullHelpNotHome(t *testing.T) {
	out, _, err := runCmd(t, "--all", "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	// --all falls through to help, not the home screen
	for _, want := range []string{"Getting started", "customer", "paywalls"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rc --all should show the full command list, missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "New here?") {
		t.Fatalf("rc --all should not show the home screen:\n%s", out)
	}
}

func TestBareRC_LoggedInNoProject_NudgesProject(t *testing.T) {
	out, _, err := runCmd(t, "--no-color", "--api-key", "sk_test")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"logged in", "no project selected", "Pick a project", "rc projects use", "Design paywalls", "rc paywalls generate"} {
		if !strings.Contains(out, want) {
			t.Fatalf("logged-in/no-project home missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "New here?") {
		t.Fatalf("authenticated home should not show first-run steps:\n%s", out)
	}
}

func TestBareRC_LoggedInWithProject_ShowsCommandMap(t *testing.T) {
	out, _, err := runCmd(t, "--no-color", "--api-key", "sk_test", "--project-id", "proj_abc")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"project proj_abc",
		"app.revenuecat.com/projects/abc", // dashboard deep-link (prefix stripped)
		"Get started", "Design paywalls", "Manage your catalog", "Manage customers & revenue", "Connect apps & integrations", "Automate with AI",
		"rc setup", "rc paywalls generate", "rc paywalls edit",
		"rc customer show <id>",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("logged-in/project home missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "New here?") || strings.Contains(out, "Pick a project") {
		t.Fatalf("home with an active project should not nudge setup:\n%s", out)
	}
}
