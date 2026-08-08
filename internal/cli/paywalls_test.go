package cli_test

import (
	"strings"
	"testing"
)

// Bare `rc paywalls` must keep printing help (scriptable) under --no-input or
// --json, never dropping into the picker — even with guided mode (npx) on.
func TestPaywalls_NonInteractiveShowsHelp(t *testing.T) {
	for _, flag := range []string{"--no-input", "--json"} {
		t.Run(flag, func(t *testing.T) {
			t.Setenv("RC_GUIDED", "1")
			stdout, _, err := runCmd(t, "paywalls", flag)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			for _, want := range []string{"Available Commands", "generate", "edit"} {
				if !strings.Contains(stdout, want) {
					t.Fatalf("expected help to contain %q, got:\n%s", want, stdout)
				}
			}
		})
	}
}
