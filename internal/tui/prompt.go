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
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"github.com/revenuecat/cli/internal/output"
)

// BrandTheme follows the CLI's color standard: ANSI semantics carry meaning
// (red is danger), RC violet is the single interaction accent (focus,
// selection, cursor), and field titles stay neutral bold so labels never
// look like errors. Brand red appears only at section landmarks, not here.
func BrandTheme() *huh.Theme {
	t := huh.ThemeBase()
	dim := lipgloss.NewStyle().Faint(true)
	// Labels stay neutral; only interaction carries the violet accent.
	t.Focused.Title = t.Focused.Title.Bold(true)
	t.Blurred.Title = t.Blurred.Title.Bold(true).Faint(true)
	t.Focused.Description = dim
	t.Blurred.Description = dim
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(output.AccentViolet).SetString("> ")
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(output.AccentViolet)
	t.Focused.SelectedPrefix = t.Focused.SelectedPrefix.Foreground(output.GreenOK)
	t.Focused.FocusedButton = t.Focused.FocusedButton.Background(output.AccentViolet).Foreground(lipgloss.Color("15")).Bold(true)
	t.Focused.BlurredButton = t.Focused.BlurredButton.Background(output.NeutralGray).Foreground(lipgloss.Color("15"))
	t.Blurred.FocusedButton = t.Focused.FocusedButton
	t.Blurred.BlurredButton = t.Focused.BlurredButton
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(output.AccentViolet)
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(output.AccentViolet)
	t.Focused.TextInput.Placeholder = dim
	t.Blurred.TextInput.Placeholder = dim
	t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.Foreground(output.ErrorRed)
	t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(output.ErrorRed)
	t.Help.ShortKey = dim
	t.Help.ShortDesc = dim
	t.Help.ShortSeparator = dim
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
	// Rhythm is owned here, not at call sites: every interactive form gets a
	// blank line above it so prompts never butt against prior output. This is
	// the only place that decision lives — it cannot be forgotten per command.
	fmt.Fprintln(os.Stderr)
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
	fmt.Fprintln(os.Stderr) // same rule as FormBuilder.Run: prompts own their spacing
	confirm := huh.NewConfirm().Title(msg).Value(&ok)
	err := huh.NewForm(huh.NewGroup(confirm)).WithTheme(BrandTheme()).WithShowHelp(false).Run()
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
