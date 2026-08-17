package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/revenuecat/cli/internal/tui"
)

// Choice is one branch of a decision: what the human picks, and the flag a
// non-interactive caller passes to select it without the prompt.
type Choice[T comparable] struct {
	Value T      // returned when this branch is chosen
	Label string // shown in the interactive picker
	Flag  string // the flag that selects this branch under --no-input (e.g. "--standalone")
}

// decide is the dual-mode primitive for a branching decision, generalizing
// requireID from "pick an entity" to "pick among labeled choices with side
// effects." It captures the three-mode contract every command owes both
// audiences, so a new decision cannot silently forget the agent path:
//
//   - preset != nil (a flag already chose): return it, no prompt
//   - interactive TTY: show title + picker, receipt the choice
//   - --no-input with nothing preset: error naming every flag, so an agent
//     is told exactly how to make this choice non-interactively
//
// Hand-rolling a tui.Select in a command is the anti-pattern this exists to
// replace: those always compile fine and always break agents.
func decide[T comparable](rt *Runtime, title string, preset *T, choices []Choice[T]) (T, error) {
	var zero T
	if preset != nil {
		return *preset, nil
	}
	if !rt.CanPrompt() {
		flags := make([]string, 0, len(choices))
		for _, c := range choices {
			if c.Flag != "" {
				flags = append(flags, c.Flag)
			}
		}
		return zero, fmt.Errorf("%s requires a choice in non-interactive mode: pass one of %s", title, strings.Join(flags, ", "))
	}
	opts := make([]huh.Option[T], len(choices))
	for i, c := range choices {
		opts[i] = huh.NewOption(c.Label, c.Value)
	}
	var chosen T
	if err := tui.Form(false).
		Field(huh.NewSelect[T]().Title(title).Options(opts...).Value(&chosen)).
		Run(); err != nil {
		return zero, err
	}
	for _, c := range choices {
		if c.Value == chosen {
			rt.Out.Answer(strings.SplitN(title, " ", 2)[0], c.Label)
			break
		}
	}
	return chosen, nil
}
