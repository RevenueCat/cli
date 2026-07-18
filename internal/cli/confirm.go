package cli

import (
	"fmt"

	"github.com/revenuecat/cli/internal/tui"
)

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
			return fmt.Errorf("aborted; %s", declineDetail[0])
		}
		return fmt.Errorf("aborted")
	}
	return nil
}
