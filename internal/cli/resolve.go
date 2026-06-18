package cli

import (
	"fmt"

	"github.com/charmbracelet/huh"

	"github.com/revenuecat/cli/internal/tui"
)

// PickerItem is a selectable entry in an interactive ID picker.
type PickerItem struct {
	ID    string
	Label string // shown in the huh.Select; should be human-readable
}

// requireID resolves a resource ID from an optional positional argument or an
// interactive searchable picker. It mirrors requireProject exactly:
//
//   - arg non-empty          → return as-is, no network call
//   - arg empty + TTY        → fetch items, show searchable picker
//   - arg empty + --no-input → error: "<noun> ID is required"
//
// Pass argAt(args, i) as arg so commands with multiple positional IDs follow
// the same pattern regardless of how many positions they need.
func requireID(rt *Runtime, arg, noun string, fetch func() ([]PickerItem, error)) (string, error) {
	if arg != "" {
		return arg, nil
	}
	if rt.Globals.NoInput || !tui.IsInteractive() {
		return "", fmt.Errorf("%s ID is required", noun)
	}
	items, err := fetch()
	if err != nil {
		return "", fmt.Errorf("listing %ss: %w", noun, err)
	}
	if len(items) == 0 {
		return "", fmt.Errorf("no %ss found — pass an ID explicitly", noun)
	}
	if len(items) == 1 {
		rt.Out.Info(fmt.Sprintf("Only one %s available: %s", noun, items[0].Label))
		return items[0].ID, nil
	}
	opts := make([]huh.Option[string], len(items))
	for i, item := range items {
		opts[i] = huh.NewOption(item.Label, item.ID)
	}
	var chosen string
	sel := huh.NewSelect[string]().
		Title("Select a " + noun).
		Description("Type to filter  ·  Enter to confirm").
		Options(opts...).
		Filtering(true).
		Value(&chosen)
	if err := tui.Form(false).Field(sel).Run(); err != nil {
		return "", err
	}
	return chosen, nil
}

// argAt returns args[i] if i is a valid index, otherwise "".
//
// Use this to pass optional positional arguments into requireID:
//
//	offeringID, err := requireID(rt, argAt(args, 0), "offering", fetchOfferings)
//	packageID,  err := requireID(rt, argAt(args, 1), "package",  fetchPackages)
func argAt(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}
