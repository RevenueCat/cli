package cli

import "testing"

// The command surface splits by output channel: humans see the curated set in
// --help, agents/JSON see everything. These guard the two invariants that
// keep that honest.

// Every name in humanSurface must be a real registered command, or the human
// help silently curates nothing.
func TestHumanSurface_NamesRealCommands(t *testing.T) {
	root := NewRootCmd("test")
	registered := map[string]bool{}
	for _, c := range root.Commands() {
		registered[c.Name()] = true
	}
	for name := range humanSurface {
		if !registered[name] {
			t.Errorf("humanSurface names %q, which is not a registered command", name)
		}
	}
}

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
