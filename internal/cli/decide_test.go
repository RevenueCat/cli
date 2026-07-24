package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/output"
)

// The two non-interactive paths of decide() are the ones agents hit, so they're
// the ones worth pinning: a preset flag short-circuits the prompt, and --no-input
// with nothing preset errors by naming every flag (never hangs waiting on a TTY).

func newTestRuntime(noInput bool) *Runtime {
	var out, errb bytes.Buffer
	return &Runtime{
		Globals: &Globals{NoInput: noInput, Version: "test"},
		Config:  &config.Config{},
		Out:     output.NewRenderer(&out, &errb, false, true, false, ""),
		Ctx:     context.Background(),
	}
}

func TestDecide_PresetShortCircuits(t *testing.T) {
	want := "duplicate"
	choices := []Choice[string]{
		{Value: "standalone", Label: "Standalone", Flag: "--standalone"},
		{Value: "duplicate", Label: "Duplicate", Flag: "--duplicate"},
	}
	got, err := decide(newTestRuntime(false), "How should the paywall be created?", &want, choices)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("preset should be returned verbatim: got %q, want %q", got, want)
	}
}

func TestDecide_NoInputErrorsNamingFlags(t *testing.T) {
	choices := []Choice[string]{
		{Value: "standalone", Label: "Standalone", Flag: "--standalone"},
		{Value: "duplicate", Label: "Duplicate", Flag: "--duplicate"},
	}
	_, err := decide[string](newTestRuntime(true), "How should the paywall be created?", nil, choices)
	if err == nil {
		t.Fatal("--no-input with no preset must error, not hang on a picker")
	}
	for _, flag := range []string{"--standalone", "--duplicate"} {
		if !strings.Contains(err.Error(), flag) {
			t.Errorf("error should tell an agent which flags to pass; missing %q in: %v", flag, err)
		}
	}
}
