package cli_test

import (
	"strings"
	"testing"
)

func TestBareRC_ShowsGettingStartedNotFullList(t *testing.T) {
	out, _, err := runCmd(t)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"New here?", "rc auth login", "rc paywalls", "rc --help"} {
		if !strings.Contains(out, want) {
			t.Fatalf("bare rc missing %q:\n%s", want, out)
		}
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
