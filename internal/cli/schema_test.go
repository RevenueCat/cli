package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

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
