package sse

import (
	"io"
	"strings"
	"testing"
)

func collect(t *testing.T, input string) []Event {
	t.Helper()
	r := NewReader(strings.NewReader(input))
	var events []Event
	for {
		event, err := r.Next()
		if err == io.EOF {
			return events
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		events = append(events, event)
	}
}

func TestReaderSingleFrame(t *testing.T) {
	events := collect(t, "data: {\"a\":1}\n\n")
	if len(events) != 1 || events[0].Data != `{"a":1}` {
		t.Fatalf("got %#v", events)
	}
}

func TestReaderNamedEventCRLF(t *testing.T) {
	events := collect(t, "event: run.started\r\ndata: {}\r\n\r\n")
	if len(events) != 1 || events[0].Name != "run.started" || events[0].Data != "{}" {
		t.Fatalf("got %#v", events)
	}
}

func TestReaderMultiLineData(t *testing.T) {
	events := collect(t, "data: line1\ndata: line2\n\n")
	if len(events) != 1 || events[0].Data != "line1\nline2" {
		t.Fatalf("got %#v", events)
	}
}

func TestReaderCommentKeepalive(t *testing.T) {
	events := collect(t, ": ping\n\ndata: x\n\n")
	if len(events) != 2 || !events[0].Comment || events[1].Data != "x" {
		t.Fatalf("got %#v", events)
	}
}

func TestReaderUnterminatedFinalFrame(t *testing.T) {
	events := collect(t, "data: tail")
	if len(events) != 1 || events[0].Data != "tail" {
		t.Fatalf("got %#v", events)
	}
}

func TestReaderSkipsBlankLines(t *testing.T) {
	events := collect(t, "\n\ndata: x\n\n")
	if len(events) != 1 || events[0].Data != "x" {
		t.Fatalf("got %#v", events)
	}
}
