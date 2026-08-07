package cli

import "testing"

// The command surface splits by output channel: humans see the curated set in
// --help, agents/JSON see everything. These guard the two invariants that
// keep that honest.

// Agent discovery (rc commands --schemas) must include agent-only commands —
// hidden from --help but present here — while excluding the punted tier.
// Hiding from humans must not hide from agents.
func TestCommandSurface_SchemaIncludesAgentOnlyExcludesPunted(t *testing.T) {
	root := NewRootCmd("test")
	tree := commandTreeWithSchemas(root)
	names := map[string]bool{}
	for _, c := range tree["commands"].([]map[string]any) {
		names[c["name"].(string)] = true
	}
	for _, agentOnly := range []string{"customer", "offerings", "entitlements", "packages"} {
		if !names[agentOnly] {
			t.Errorf("agent-only command %q missing from schema surface", agentOnly)
		}
	}
	if names["setup"] {
		t.Error("punted command 'setup' should not appear in the schema surface")
	}
}

// The human-help curation runs behind a testing.Testing() short-circuit, so
// this drives curateSurface directly: the full surface shows by default, only
// punted is held back, aliases stay hidden, and --all reveals punted too.
func TestCurateSurface(t *testing.T) {
	root := NewRootCmd("test")
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
	if !hidden("login") {
		t.Error("the back-compat 'login' alias should stay hidden")
	}
	if !hidden("setup") {
		t.Error("punted 'setup' should be hidden by default")
	}

	curateSurface(root, true) // rc --all
	if hidden("setup") {
		t.Error("--all should reveal punted 'setup'")
	}
}
