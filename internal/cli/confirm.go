package cli

import (
	"fmt"

	"github.com/revenuecat/cli/internal/tui"
)

// requireInteractive gates the most destructive, irreversible commands to a
// person at a real terminal. Unlike confirmOrAbort, --yes cannot bypass it and
// there is no non-interactive path: --json, --no-input, and non-TTY sessions
// are all refused. This keeps automation from firing deletes that are hard or
// impossible to undo — those stay a deliberate, human-only action.
func requireInteractive(rt *Runtime, action string) error {
	if rt.CanPrompt() {
		return nil
	}
	return WithHint(
		fmt.Errorf("%s is interactive-only: it needs a real terminal and can't run with --json or --no-input", action),
		"Run it yourself in an interactive terminal. It is intentionally unavailable to automation because it is irreversible.",
	)
}

// confirmOrAbort is the one way a command asks consent before acting. It owns
// the full contract in one place — --yes skips it, --no-input without --yes
// fails it, declining aborts with a uniform error — so no command can
// reimplement consent slightly differently. The design-boundaries test keeps
// rt.Globals.AssumeYes out of command files to enforce this.
func confirmOrAbort(rt *Runtime, msg string, declineDetail ...string) error {
	if rt.Globals.AssumeYes {
		return nil
	}
	ok, err := tui.Confirm(rt.Globals.NoInput, msg)
	if err != nil {
		return err
	}
	if !ok {
		if len(declineDetail) > 0 && declineDetail[0] != "" {
			rt.Out.Hint(declineDetail[0])
		}
		return fmt.Errorf("aborted")
	}
	return nil
}
