// Package sse implements a streaming Server-Sent Events reader shared by the
// Rico and Astra agent clients.
package sse

import (
	"bufio"
	"io"
	"strings"
)

// Event is one SSE frame. Comment-only frames (keepalives) are surfaced with
// Comment set so callers can reset inactivity timers.
type Event struct {
	Name    string // value of the "event:" field, if any
	Data    string // "data:" lines joined with "\n"
	Comment bool   // frame contained only ":" comment lines
}

// Reader incrementally decodes SSE frames from a stream.
type Reader struct {
	scanner *bufio.Scanner
}

const maxLineBytes = 25 << 20

func NewReader(r io.Reader) *Reader {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), maxLineBytes)
	return &Reader{scanner: scanner}
}

// Next returns the next frame, or io.EOF when the stream ends. Frames with no
// data and no comment (stray blank lines) are skipped.
func (r *Reader) Next() (Event, error) {
	var event Event
	sawComment := false
	sawLine := false
	var data []string
	for r.scanner.Scan() {
		line := strings.TrimSuffix(r.scanner.Text(), "\r")
		if line == "" {
			if len(data) > 0 || event.Name != "" {
				event.Data = strings.Join(data, "\n")
				return event, nil
			}
			if sawComment {
				return Event{Comment: true}, nil
			}
			sawLine = false
			continue
		}
		sawLine = true
		switch {
		case strings.HasPrefix(line, ":"):
			sawComment = true
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		case strings.HasPrefix(line, "event:"):
			event.Name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		}
	}
	if err := r.scanner.Err(); err != nil {
		return Event{}, err
	}
	if sawLine && (len(data) > 0 || event.Name != "") {
		event.Data = strings.Join(data, "\n")
		return event, nil
	}
	return Event{}, io.EOF
}
