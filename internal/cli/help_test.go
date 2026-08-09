package cli_test

import (
	"strings"
	"testing"
)

func TestSubcommandHelp_CollapsesGlobalFlags(t *testing.T) {
	out, _, err := runCmd(t, "rico", "--help", "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Available Commands:", "chat",
		"Global flags: ", "--api-key", "--json",
		"Run `rc --help` for what each global flag does.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("subcommand help missing %q:\n%s", want, out)
		}
	}
	// The full inherited-flag descriptions must NOT be repeated on subcommands
	// — that's the block we collapsed.
	for _, unwanted := range []string{"RevenueCat API key", "emit machine-readable JSON output"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("subcommand help should not repeat global-flag descriptions, found %q:\n%s", unwanted, out)
		}
	}
}

func TestNestedSubcommandHelp_KeepsMidLevelInheritedFlags(t *testing.T) {
	// `rc rico conversations` defines a persistent --base-url, inherited by its
	// leaves. It's not a root global, so the root pointer wouldn't document it —
	// it must show in full, not get folded into the "Global flags:" summary.
	out, _, err := runCmd(t, "rico", "conversations", "show", "--help", "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "--base-url") || !strings.Contains(out, "Rico endpoint") {
		t.Fatalf("nested help dropped the mid-level inherited --base-url description:\n%s", out)
	}
	// Root globals are still collapsed.
	if !strings.Contains(out, "Global flags: ") {
		t.Fatalf("nested help missing the global-flags summary:\n%s", out)
	}
}

func TestRootHelp_KeepsFullGlobalFlagDescriptions(t *testing.T) {
	out, _, err := runCmd(t, "--help", "--no-color")
	if err != nil {
		t.Fatal(err)
	}
	// At the root these flags are local, so their full descriptions stay.
	for _, want := range []string{"--api-key string", "RevenueCat API key", "emit machine-readable JSON output"} {
		if !strings.Contains(out, want) {
			t.Fatalf("root help missing %q:\n%s", want, out)
		}
	}
	// Root commands render in labeled groups, not one flat "Available Commands" list.
	for _, want := range []string{"Getting started", "Customers & revenue", "Advanced"} {
		if !strings.Contains(out, want) {
			t.Fatalf("root help missing command group %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Available Commands:") {
		t.Fatalf("root help should use groups, not a flat Available Commands list:\n%s", out)
	}
}
