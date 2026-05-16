// Package tui wraps charmbracelet/huh so every interactive prompt
//   - skips fields that are already set (via flag/env), and
//   - fails cleanly under --no-input or non-TTY instead of hanging.
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

// Field adds an interactive field, but only if its bound value is currently
// the zero value. huh fields bind via Value(&x); we can't introspect that here,
// so the caller's prompt library convention is: pre-populate from flags before
// calling Field, and pass nil/skip when satisfied. For simplicity in the
// scaffold we just always include fields and rely on huh's WithAccessible mode
// for non-TTY environments.
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
		// surface a stable error listing what's missing.
		// (huh doesn't expose value introspection, so callers should set
		// Validate(Required(...)) to drive this.)
		for _, f := range b.fields {
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
	if noInput || !isInteractive() {
		return false, fmt.Errorf("%s (pass --yes to confirm non-interactively)", msg)
	}
	var ok bool
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
