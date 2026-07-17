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

	"github.com/revenuecat/cli/internal/output"
)

// BrandTheme is huh's Charm theme recolored with the RevenueCat palette so
// every prompt, picker, and confirm reads as the same product.
func BrandTheme() *huh.Theme {
	t := huh.ThemeCharm()
	t.Focused.Title = t.Focused.Title.Foreground(output.BrandRed).Bold(true)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(output.BrandRed)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(output.BrandRed)
	t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.Foreground(output.GreenOK)
	t.Focused.FocusedButton = t.Focused.FocusedButton.Background(output.BrandRed)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(output.BrandRed)
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(output.BrandRed)
	t.Focused.Base = t.Focused.Base.BorderForeground(output.BrandRed)
	return t
}

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
	form := huh.NewForm(huh.NewGroup(b.fields...)).WithTheme(BrandTheme())
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
	confirm := huh.NewConfirm().Title(msg).Value(&ok)
	err := huh.NewForm(huh.NewGroup(confirm)).WithTheme(BrandTheme()).Run()
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
