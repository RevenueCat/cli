package cli

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
)

// Command surface: humans see a short curated list; agents and --json/--schema
// see everything. The split rides on the output channel, not an env var —
// `rc --help` is scoped, `rc commands --schemas` is full — so agent discovery
// never depends on a flag being set in a launch context we don't control.
//
// Three tiers, set via the "surface" annotation:
//   - human (default): shown in --help and everywhere
//   - agent-only: hidden from --help, still in commands/schema and still
//     runnable (hidden != disabled, so skills naming it keep working)
//   - punted: hidden everywhere including schema, until DX-tested
//     (the one-shot `setup` orchestrator)

const (
	annotationSurface = "surface"
	surfacePunted     = "punted"
)

// humanSurface is the curated set shown by default; everything else is
// agent-only. Keep it tight — it's the first thing a human sees.
var humanSurface = map[string]bool{
	"paywalls": true,
	"capital":  true,
	"auth":     true,
	"projects": true,
	"apps":     true,
	"login":    true, // alias
	"version":  true,
}

func showAllSurface(root *cobra.Command) bool {
	if os.Getenv("RC_SURFACE") == "full" {
		return true
	}
	all, _ := root.PersistentFlags().GetBool("all")
	return all
}

// applySurfaceProfile hides non-human commands from --help. Runs after flags
// parse: --all (or RC_SURFACE=full) reveals everything; punted commands stay
// hidden regardless. Hidden commands still execute, and commands/schema
// include them (they gate on the punted annotation, not Hidden).
func applySurfaceProfile(root *cobra.Command) {
	if testing.Testing() {
		return // tests assert the full surface; the rules themselves are
		// covered directly by TestCurateSurface.
	}
	curateSurface(root, showAllSurface(root))
}

// curateSurface applies the visibility rules. Split out of applySurfaceProfile
// so the rules are testable without the testing.Testing() short-circuit, which
// would otherwise let curation regress with nothing catching it.
func curateSurface(root *cobra.Command, all bool) {
	for _, cmd := range root.Commands() {
		switch {
		case cmd.Annotations[annotationSurface] == surfacePunted:
			cmd.Hidden = true
		case all:
			cmd.Hidden = false
		case !humanSurface[cmd.Name()]:
			cmd.Hidden = true
		}
	}
}

// puntedFromSchema reports whether a command is hidden even from agent
// discovery. Only the punted tier qualifies; agent-only commands are hidden
// from help but must still appear in commands/schema.
func puntedFromSchema(c *cobra.Command) bool {
	return c.Annotations[annotationSurface] == surfacePunted
}
