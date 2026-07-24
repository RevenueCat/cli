// Package tui wraps charmbracelet/huh to ensure every interactive prompt fails
// cleanly under --no-input or non-TTY instead of hanging.
//
// Field skipping (when values are already set via flags/env) is the caller's
// responsibility — they must check the value before calling Field().
//
// This is the linchpin of dual-use (human + LLM): one command surface, two
// modes of driving it.
package tui

import (
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

type FormBuilder struct {
	noInput bool
	fields  []huh.Field
}

func Form(noInput bool) *FormBuilder {
	return &FormBuilder{noInput: noInput}
}

// Field adds an interactive field to the form. It unconditionally appends the
// field — callers must decide whether to call this based on whether the value
// is already set (e.g. via flag). Under --no-input, fields are validated via
// their attached Validate() functions; required fields should use
// Validate(Required(...)) to enforce presence in non-interactive mode.
func (b *FormBuilder) Field(f huh.Field) *FormBuilder {
	b.fields = append(b.fields, f)
	return b
}

func (b *FormBuilder) Run() error {
	if len(b.fields) == 0 {
		return nil
	}
	if b.noInput || !isInteractive() {
		// Validate each field's bound value; if any required value is unset,
		// surface a stable error listing what's missing. A field's Error() is
		// only populated once its validator has run, and the form never runs
		// here — so Blur() the field first to force validation against its
		// bound value (seeded from the flag at construction, so this reads the
		// real value and doesn't clobber it). Without this, Validate(Required)
		// is a no-op under --no-input and empty required inputs slip through.
		for _, f := range b.fields {
			f.Blur()
			if err := f.Error(); err != nil {
				return fmt.Errorf("missing required input (non-interactive): %w", err)
			}
		}
		return nil
	}
	form := huh.NewForm(huh.NewGroup(b.fields...))
	return form.Run()
}

func Confirm(noInput bool, msg string) (bool, error) {
	return ConfirmDefault(noInput, msg, false)
}

func ConfirmDefault(noInput bool, msg string, defaultValue bool) (bool, error) {
	if noInput || !isInteractive() {
		return false, fmt.Errorf("%s (pass --yes to confirm non-interactively)", msg)
	}
	ok := defaultValue
	err := huh.NewConfirm().Title(msg).Value(&ok).Run()
	return ok, err
}

func Required(label string) func(string) error {
	return func(s string) error {
		if s == "" {
			return errors.New(label + " is required")
		}
		return nil
	}
}

func isInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// IsInteractive reports whether both stdin and stdout are connected to a TTY.
func IsInteractive() bool { return isInteractive() }
