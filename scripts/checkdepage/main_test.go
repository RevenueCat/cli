package main

import (
	"strings"
	"testing"
	"time"
)

func TestFindTooNew(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	window := 7 * 24 * time.Hour

	input := `
{"Path":"example.com/main","Main":true}
{"Path":"example.com/old","Version":"v1.0.0","Time":"2026-01-01T00:00:00Z"}
{"Path":"example.com/fresh","Version":"v2.3.4","Time":"2026-08-24T00:00:00Z"}
{"Path":"example.com/edge","Version":"v0.1.0","Time":"2026-08-19T13:00:00Z"}
{"Path":"example.com/exact","Version":"v0.2.0","Time":"2026-08-19T12:00:00Z"}
{"Path":"example.com/ignored","Version":"v9.9.9","Time":"2026-08-26T00:00:00Z"}
{"Path":"example.com/notime","Version":"v1.2.3"}
{"Path":"example.com/wrapper","Version":"v1.0.0","Time":"2026-01-01T00:00:00Z","Replace":{"Path":"example.com/replacement","Version":"v0.0.1","Time":"2026-08-25T00:00:00Z"}}
`
	ignore := map[string]bool{"example.com/ignored": true}
	got, err := findTooNew(strings.NewReader(input), now, window, ignore)
	if err != nil {
		t.Fatalf("findTooNew: %v", err)
	}

	// example.com/exact is exactly window-old and must NOT be flagged (strict <),
	// so it's absent here; the len check below catches it if that regresses.
	want := map[string]bool{
		"example.com/fresh":       true,
		"example.com/edge":        true, // ~6d23h old, just inside the window
		"example.com/replacement": true, // replacement time is what counts
	}
	if len(got) != len(want) {
		t.Fatalf("got %d findings, want %d: %+v", len(got), len(want), got)
	}
	for _, f := range got {
		if !want[f.Path] {
			t.Errorf("unexpected finding: %s %s", f.Path, f.Version)
		}
	}
	// youngest first
	if got[0].Path != "example.com/replacement" || got[len(got)-1].Path != "example.com/edge" {
		t.Errorf("not sorted youngest-first: %+v", got)
	}
}

func TestFindTooNewAllOld(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	input := `{"Path":"example.com/a","Version":"v1.0.0","Time":"2026-01-01T00:00:00Z"}`
	got, err := findTooNew(strings.NewReader(input), now, 7*24*time.Hour, nil)
	if err != nil {
		t.Fatalf("findTooNew: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no findings, got %+v", got)
	}
}
