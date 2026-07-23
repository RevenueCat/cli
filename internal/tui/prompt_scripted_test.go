package tui

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/huh"
)

// Proves the interactive layer is unit-testable: drive a real huh Select with
// scripted keystrokes and assert the bound value. This is the seam that lets
// us cover picker/guided flows instead of verifying them only by screenshot.
func TestFormScriptedIO_DrivesSelect(t *testing.T) {
	run := func(keys string) string {
		var choice string
		done := make(chan error, 1)
		go func() {
			done <- Form(false).
				Field(huh.NewSelect[string]().
					Title("Pick").
					Options(
						huh.NewOption("alpha", "alpha"),
						huh.NewOption("beta", "beta"),
						huh.NewOption("gamma", "gamma"),
					).
					Value(&choice)).
				WithScriptedIO(strings.NewReader(keys), io.Discard).
				Run()
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("scripted run: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("scripted form hung")
		}
		return choice
	}

	// "\x1b[B" = down arrow, "\r" = enter.
	if got := run("\r"); got != "alpha" {
		t.Errorf("enter on first option = %q, want alpha", got)
	}
	if got := run("\x1b[B\x1b[B\r"); got != "gamma" {
		t.Errorf("down down enter = %q, want gamma", got)
	}
}
