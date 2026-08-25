package tui

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// railPrompt runs one rail prompt with scripted keystrokes, guarding against a
// hang, and returns the rendered output for gutter assertions. keys uses raw
// terminal input: "\x1b[B" = down arrow, "\r" = enter, "\x03" = ctrl-c.
func railPrompt(t *testing.T, keys string, fn func(fl *Flow) error) string {
	t.Helper()
	asciiProfile(t)
	var buf bytes.Buffer
	fl := &Flow{w: &buf, in: strings.NewReader(keys)}
	done := make(chan error, 1)
	go func() { done <- fn(fl) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("scripted prompt: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("scripted rail prompt hung")
	}
	return buf.String()
}

func assertGutter(t *testing.T, out string) {
	t.Helper()
	if !strings.Contains(out, "│") {
		t.Errorf("prompt output is not on the rail gutter:\n%q", out)
	}
}

func TestRailSelectScripted(t *testing.T) {
	opts := []Option{{Label: "alpha", Value: "a"}, {Label: "beta", Value: "b"}, {Label: "gamma", Value: "c"}}
	var got string
	out := railPrompt(t, "\r", func(fl *Flow) error {
		v, err := fl.Select("Pick", opts)
		got = v
		return err
	})
	if got != "a" {
		t.Errorf("enter on first option = %q, want a", got)
	}
	assertGutter(t, out)

	out = railPrompt(t, "\x1b[B\x1b[B\r", func(fl *Flow) error {
		v, err := fl.Select("Pick", opts)
		got = v
		return err
	})
	if got != "c" {
		t.Errorf("down down enter = %q, want c", got)
	}
	assertGutter(t, out)
}

func TestRailConfirmScripted(t *testing.T) {
	var got bool
	// 'y' accepts regardless of default.
	railPrompt(t, "y", func(fl *Flow) error {
		v, err := fl.Confirm("Proceed?", false)
		got = v
		return err
	})
	if !got {
		t.Errorf("'y' = %v, want true", got)
	}
	// 'n' declines.
	railPrompt(t, "n", func(fl *Flow) error {
		v, err := fl.Confirm("Proceed?", true)
		got = v
		return err
	})
	if got {
		t.Errorf("'n' = %v, want false", got)
	}
}

func TestRailInputScripted(t *testing.T) {
	var got string
	out := railPrompt(t, "hello@example.com\r", func(fl *Flow) error {
		v, err := fl.Input("Email", "you@example.com", nil)
		got = v
		return err
	})
	if got != "hello@example.com" {
		t.Errorf("input = %q, want hello@example.com", got)
	}
	assertGutter(t, out)
}

func TestRailCancelReturnsErr(t *testing.T) {
	// Esc/ctrl-c must surface ErrPromptCancelled, never a silent zero value.
	err := func() error {
		asciiProfile(t)
		fl := &Flow{w: &bytes.Buffer{}, in: strings.NewReader("\x03")}
		done := make(chan error, 1)
		go func() { _, e := fl.Select("Pick", []Option{{Label: "a", Value: "a"}}); done <- e }()
		select {
		case e := <-done:
			return e
		case <-time.After(5 * time.Second):
			t.Fatal("scripted cancel hung")
			return nil
		}
	}()
	if err != ErrPromptCancelled {
		t.Errorf("ctrl-c = %v, want ErrPromptCancelled", err)
	}
}
