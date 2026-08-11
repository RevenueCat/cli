package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

// The command surface splits by output channel: humans see the curated set in
// --help, agents/JSON see everything. These guard the two invariants that
// keep that honest.

// Agent discovery (rc commands --schemas) must include every non-experimental
// command, including setup (agents run it non-interactively for the prompt).
func TestCommandSurface_SchemaIncludesAgentCommands(t *testing.T) {
	root := NewRootCmd("test")
	tree := commandTreeWithSchemas(root)
	names := map[string]bool{}
	for _, c := range tree["commands"].([]map[string]any) {
		names[c["name"].(string)] = true
	}
	for _, want := range []string{"customer", "offerings", "entitlements", "packages", "setup"} {
		if !names[want] {
			t.Errorf("command %q missing from schema surface", want)
		}
	}
}

// The human-help curation runs behind a testing.Testing() short-circuit, so
// this drives curateSurface directly: the full surface shows by default, a
// experimental command is held back until --all, and aliases stay hidden.
func TestCurateSurface(t *testing.T) {
	root := NewRootCmd("test")
	root.AddCommand(&cobra.Command{Use: "x-experimental", Annotations: map[string]string{annotationSurface: surfaceExperimental}})
	hidden := func(name string) bool {
		for _, c := range root.Commands() {
			if c.Name() == name {
				return c.Hidden
			}
		}
		t.Fatalf("command %q not registered", name)
		return false
	}

	curateSurface(root, false) // default help
	if hidden("paywalls") {
		t.Error("'paywalls' should be visible in --help")
	}
	if hidden("customer") {
		t.Error("'customer' should be visible in --help (full surface)")
	}
	if hidden("setup") {
		t.Error("'setup' should be visible in --help")
	}
	if !hidden("login") {
		t.Error("the back-compat 'login' alias should stay hidden")
	}
	if !hidden("x-experimental") {
		t.Error("a experimental command should be hidden by default")
	}

	curateSurface(root, true) // rc --all
	if hidden("x-experimental") {
		t.Error("--all should reveal a experimental command")
	}
}

func TestEveryCommandHasARegisteredGroup(t *testing.T) {
	valid := map[string]bool{}
	for _, g := range commandGroups {
		valid[g.ID] = true
	}
	root := NewRootCmd("test")
	for _, c := range root.Commands() {
		switch c.Name() {
		case "help", "completion":
			continue
		}
		if c.GroupID == "" {
			t.Errorf("command %q has no group — set one in NewRootCmd's grouped table", c.Name())
			continue
		}
		if !valid[c.GroupID] {
			t.Errorf("command %q has group %q not in commandGroups", c.Name(), c.GroupID)
		}
	}
}
