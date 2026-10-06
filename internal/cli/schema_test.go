package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestExperimentConfigSchemaExplainsNestedFields(t *testing.T) {
	root := NewRootCmd("test")
	for _, path := range []string{"experiments create", "experiments update"} {
		data, err := json.Marshal(commandSchema(findCommand(t, root, path))["config_fields"])
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"primary_metric", "new_and_existing", "experiment_duration_settings", "placement_offerings", "app_version", "context"} {
			if !strings.Contains(string(data), want) {
				t.Errorf("%s schema missing %q", path, want)
			}
		}
	}
}

func TestTargetingConfigSchemaExplainsConditionsAndRuleTypes(t *testing.T) {
	root := NewRootCmd("test")
	for _, path := range []string{"targeting create", "targeting update"} {
		config := commandSchema(findCommand(t, root, path))["config_fields"].(map[string]any)
		data, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"conditions", "app_version", "context", "placement_offerings", "fallback_offering_id", "required_for_legacy", "start_date", "schedule", "position"} {
			if !strings.Contains(string(data), want) {
				t.Errorf("%s schema missing %q", path, want)
			}
		}
		audience := config["properties"].(map[string]any)["audience_id"].(map[string]any)
		if !contains(audience["type"].([]string), "null") || !strings.Contains(audience["description"].(string), "conditions to []") {
			t.Errorf("%s schema must explain how to switch between audiences and conditions", path)
		}
	}
}

func TestTargetingUpdateSchemaIncludesCheckpointFieldsAndLimits(t *testing.T) {
	root := NewRootCmd("test")
	config := commandSchema(findCommand(t, root, "targeting update"))["config_fields"].(map[string]any)
	fields := config["properties"].(map[string]any)
	if fields["flow_id"] == nil {
		t.Fatal("checkpoint Flow field is missing")
	}
	checkpoints := fields["checkpoints"].(map[string]any)
	if checkpoints["minItems"] != 1 || checkpoints["maxItems"] != 1 || !strings.Contains(config["description"].(string), "audience_id (non-null)") {
		t.Fatalf("checkpoint update constraints are missing: %v", config)
	}
}

func findCommand(t *testing.T, root *cobra.Command, path string) *cobra.Command {
	t.Helper()
	cur := root
	if path == "" {
		return cur
	}
	for _, name := range strings.Fields(path) {
		var next *cobra.Command
		for _, sc := range cur.Commands() {
			if sc.Name() == name {
				next = sc
				break
			}
		}
		if next == nil {
			t.Fatalf("command %q not found (stuck at %q)", path, cur.Name())
		}
		cur = next
	}
	return cur
}

func capsOf(t *testing.T, root *cobra.Command, path string) []string {
	t.Helper()
	return inferCapabilities(findCommand(t, root, path))
}

func contains(caps []string, want string) bool {
	for _, c := range caps {
		if c == want {
			return true
		}
	}
	return false
}

func TestInferCapabilities_NestedPriceWriteSurfaces(t *testing.T) {
	root := NewRootCmd("test")

	priceCaps := capsOf(t, root, "products prices")
	if !contains(priceCaps, "set") {
		t.Errorf("products prices should expose a `set` capability, got %v", priceCaps)
	}

	productCaps := capsOf(t, root, "products")
	if !contains(productCaps, "prices:set") {
		t.Errorf("products should aggregate the nested `prices:set` capability, got %v", productCaps)
	}
}

func TestInferCapabilities_NoActionableGroupIsBlank(t *testing.T) {
	root := NewRootCmd("test")

	cases := []struct {
		path string
		want string
	}{
		{"auth", "login"},
		{"apps apple", "setup"},
		{"apps apple", "check"},
		{"rico", "rico"},
	}
	for _, tc := range cases {
		caps := capsOf(t, root, tc.path)
		if len(caps) == 0 {
			t.Errorf("%q reported no capabilities; a command with actions must not be blank", tc.path)
			continue
		}
		if !contains(caps, tc.want) {
			t.Errorf("%q capabilities %v missing expected %q", tc.path, caps, tc.want)
		}
	}
}

func TestCanonicalVerb_FoldsGetToShow(t *testing.T) {
	if got := canonicalVerb("get"); got != "show" {
		t.Errorf("canonicalVerb(get) = %q, want show", got)
	}
	if got := canonicalVerb("sync"); got != "sync" {
		t.Errorf("canonicalVerb(sync) = %q, want sync", got)
	}
}

func TestInferCapabilities_DriftGuard(t *testing.T) {
	root := NewRootCmd("test")

	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if isExperimental(c) {
			return
		}

		if hasRunnableDescendant(c) && len(inferCapabilities(c)) == 0 {
			t.Errorf("%q has actionable subcommands but reports no capabilities", commandPath(c))
		}

		if len(c.Commands()) > 0 {
			caps := inferCapabilities(c)
			for _, sc := range c.Commands() {
				if isExperimental(sc) || !isDiscoverableRunnable(sc) {
					continue
				}
				want := canonicalVerb(sc.Name())
				if !contains(caps, want) {
					t.Errorf("runnable command %q not represented in %q capabilities %v (want %q)",
						commandPath(sc), commandPath(c), caps, want)
				}
			}
		}

		for _, sc := range c.Commands() {
			walk(sc)
		}
	}
	walk(root)
}

func hasRunnableDescendant(c *cobra.Command) bool {
	for _, sc := range c.Commands() {
		if isExperimental(sc) {
			continue
		}
		if sc.Runnable() || hasRunnableDescendant(sc) {
			return true
		}
	}
	return false
}

func TestTargetingSchemaDocumentsNullableFields(t *testing.T) {
	fields := targetingConfigFields(false)["properties"].(map[string]any)
	schedule := fields["schedule"].(map[string]any)["properties"].(map[string]any)
	placements := fields["placements"].(map[string]any)
	placementFields := placements["properties"].(map[string]any)
	itemFields := placementFields["placement_offerings"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	for name, value := range map[string]any{"audience_id": fields["audience_id"], "schedule": fields["schedule"], "start_date": schedule["start_date"], "end_date": schedule["end_date"], "placements": placements, "fallback_offering_id": placementFields["fallback_offering_id"], "placement offering_id": itemFields["offering_id"]} {
		types, ok := value.(map[string]any)["type"].([]string)
		if !ok || !contains(types, "null") {
			t.Errorf("%s does not expose null: %v", name, value)
		}
	}
}
