package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/huh"
)

// The linchpin of dual-use: under --no-input, a required field whose bound value
// is unset must fail with a clear error (never hang, never silently pass). This
// guards the subtle bug where Validate(Required(...)) was a no-op because a
// field's Error() is only populated after its validator runs.

func TestForm_NoInput_MissingRequiredFails(t *testing.T) {
	var name string // empty: the flag was never provided
	err := Form(true).
		Field(huh.NewInput().Title("Name").Value(&name).Validate(Required("name"))).
		Run()
	if err == nil {
		t.Fatal("want error for missing required field under --no-input")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("error should name the missing field, got: %v", err)
	}
}

func TestForm_NoInput_SatisfiedRequiredPasses(t *testing.T) {
	name := "Pro" // as if set via --name flag before the form was built
	err := Form(true).
		Field(huh.NewInput().Title("Name").Value(&name).Validate(Required("name"))).
		Run()
	if err != nil {
		t.Fatalf("a satisfied required field should pass under --no-input, got: %v", err)
	}
	if name != "Pro" {
		t.Fatalf("validation must not clobber the bound value, got %q", name)
	}
}
