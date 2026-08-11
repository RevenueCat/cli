package cli

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
)

// Command surface: --help shows the full command set; only the experimental tier
// (commands not yet DX-tested) is held back, and `rc --all` reveals those too.
// Back-compat aliases stay hidden via their own Hidden flag — curateSurface
// never un-hides them.
//
// Two annotation tiers via "surface":
//   - default: shown in --help, commands/schema, and runnable
//   - experimental: hidden from --help and schema until DX-tested (the one-shot
//     `setup` orchestrator); `rc --all` shows it in help but never schema

const (
	annotationSurface   = "surface"
	surfaceExperimental = "experimental"
)

func showAllSurface(root *cobra.Command) bool {
	if os.Getenv("RC_SURFACE") == "full" {
		return true
	}
	all, _ := root.PersistentFlags().GetBool("all")
	return all
}

// applySurfaceProfile hides the experimental tier from --help unless --all (or
// RC_SURFACE=full) is set. Everything else is visible. Runs after flags parse.
func applySurfaceProfile(root *cobra.Command) {
	if testing.Testing() {
		return // tests assert the full surface; the rule is covered directly
		// by TestCurateSurface.
	}
	curateSurface(root, showAllSurface(root))
}

// curateSurface applies the visibility rule. Split out of applySurfaceProfile
// so it's testable without the testing.Testing() short-circuit. Only experimental
// commands are touched; aliases and every other command keep their own Hidden
// state (so the full surface shows and aliases stay hidden).
func curateSurface(root *cobra.Command, all bool) {
	for _, cmd := range root.Commands() {
		if cmd.Annotations[annotationSurface] == surfaceExperimental {
			cmd.Hidden = !all
		}
	}
}

// experimentalFromSchema reports whether a command is hidden even from agent
// discovery. Only the experimental tier qualifies; agent-only commands are hidden
// from help but must still appear in commands/schema.
func experimentalFromSchema(c *cobra.Command) bool {
	return c.Annotations[annotationSurface] == surfaceExperimental
}
