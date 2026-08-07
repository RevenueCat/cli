package cli_test

import (
	"strings"
	"testing"
)

// Bare `rc paywalls` under --no-input must keep printing help (scriptable),
// never drop into the picker — even with guided mode (npx) on.
func TestPaywalls_NoInputShowsHelp(t *testing.T) {
	t.Setenv("RC_GUIDED", "1")
	stdout, _, err := runCmd(t, "paywalls", "--no-input")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	for _, want := range []string{"Available Commands", "generate", "edit"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("expected help to contain %q, got:\n%s", want, stdout)
		}
	}
}
