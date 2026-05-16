package output_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/output"
)

// newR builds a Renderer wired to in-memory buffers so tests can assert the
// stdout=data / stderr=chatter contract without touching real I/O.
func newR(jsonMode bool) (*output.Renderer, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	r := output.NewRenderer(&out, &errb, jsonMode, true, "")
	return r, &out, &errb
}

func TestRender_JSONMode_WrapsInEnvelope(t *testing.T) {
	r, out, errb := newR(true)
	if err := r.Render(map[string]any{"id": "x"}); err != nil {
		t.Fatal(err)
	}
	if errb.Len() != 0 {
		t.Errorf("JSON mode must not write to stderr; got %q", errb.String())
	}
	var got struct {
		Data          map[string]any `json:"data"`
		SchemaVersion int            `json:"schema_version"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output not valid JSON: %v\nbody:%s", err, out.String())
	}
	if got.SchemaVersion != 1 {
		t.Errorf("want schema_version=1, got %d", got.SchemaVersion)
	}
	if got.Data["id"] != "x" {
		t.Errorf("payload not under .data: %+v", got)
	}
}

func TestRender_PrettyMode_WritesToStdoutOnly(t *testing.T) {
	r, out, errb := newR(false)
	if err := r.Render(map[string]string{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	if errb.Len() != 0 {
		t.Errorf("data must not go to stderr; got %q", errb.String())
	}
	if !strings.Contains(out.String(), `"k"`) || !strings.Contains(out.String(), `"v"`) {
		t.Errorf("expected k/v in stdout; got %q", out.String())
	}
}

func TestRenderTable_EmptyGoesToStderr(t *testing.T) {
	r, out, errb := newR(false)
	if err := r.RenderTable(output.Table{Columns: []string{"A"}}); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("empty table should not write data to stdout; got %q", out.String())
	}
	if !strings.Contains(errb.String(), "no results") {
		t.Errorf("expected 'no results' on stderr; got %q", errb.String())
	}
}

func TestRenderTable_AlignsColumnsByWidestRow(t *testing.T) {
	r, out, _ := newR(false)
	err := r.RenderTable(output.Table{
		Columns: []string{"ID", "NAME"},
		Rows: [][]string{
			{"x", "short"},
			{"yyyyy", "a-lot-longer"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines (header + 2 rows), got %d:\n%s", len(lines), out.String())
	}
	// All rows should have equal width (alignment contract).
	if len(lines[0]) != len(lines[1]) || len(lines[1]) != len(lines[2]) {
		t.Errorf("rows are not aligned:\n%q\n%q\n%q", lines[0], lines[1], lines[2])
	}
}

func TestRenderTable_JSONMode_EmitsRawNotTable(t *testing.T) {
	r, out, _ := newR(true)
	raw := map[string]any{"items": []any{map[string]any{"id": "x"}}}
	err := r.RenderTable(output.Table{
		Columns: []string{"ID"},
		Rows:    [][]string{{"x"}},
		Raw:     raw,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	items, ok := got.Data["items"].([]any)
	if !ok || len(items) != 1 {
		t.Errorf("expected raw items passed through, got %+v", got.Data)
	}
}

func TestRender_FormatExtractsScalar(t *testing.T) {
	var out, errb bytes.Buffer
	r := output.NewRenderer(&out, &errb, true, true, ".data.id")
	if err := r.Render(map[string]any{"id": "cus_abc"}); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(out.String())
	if got != "cus_abc" {
		t.Errorf("want shell-friendly unquoted scalar 'cus_abc', got %q", got)
	}
}

func TestRender_FormatExtractsObject(t *testing.T) {
	var out, errb bytes.Buffer
	r := output.NewRenderer(&out, &errb, true, true, ".data.items[]")
	err := r.Render(map[string]any{"items": []any{
		map[string]any{"id": "a"},
		map[string]any{"id": "b"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 NDJSON lines, got %d:\n%s", len(lines), out.String())
	}
	if !strings.Contains(lines[0], `"id":"a"`) || !strings.Contains(lines[1], `"id":"b"`) {
		t.Errorf("output: %v", lines)
	}
}

func TestRender_FormatBadExpression_ReturnsErrBadFormat(t *testing.T) {
	var out, errb bytes.Buffer
	r := output.NewRenderer(&out, &errb, true, true, "..invalid..")
	err := r.Render(map[string]any{"id": "x"})
	if err == nil {
		t.Fatal("want error for invalid jq expression")
	}
	if !errors.Is(err, output.ErrBadFormat) {
		t.Errorf("want errors.Is(err, ErrBadFormat); got %T: %v", err, err)
	}
}

func TestRender_FormatWithoutJSON_WarnsAndFallsThrough(t *testing.T) {
	var out, errb bytes.Buffer
	r := output.NewRenderer(&out, &errb, false, true, ".id")
	if err := r.Render(map[string]any{"id": "x"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errb.String(), "--format is only applied to --json") {
		t.Errorf("expected warning on stderr; got %q", errb.String())
	}
	// stdout should still have the pretty-JSON fallback, not the filtered output.
	if !strings.Contains(out.String(), `"id"`) {
		t.Errorf("expected fallback pretty output; got %q", out.String())
	}
}

func TestStatusHelpers_GoToStderrAndAreSuppressedInJSON(t *testing.T) {
	cases := []struct {
		name string
		call func(r *output.Renderer)
	}{
		{"success", func(r *output.Renderer) { r.Success("hi") }},
		{"info", func(r *output.Renderer) { r.Info("hi") }},
		{"warn", func(r *output.Renderer) { r.Warn("hi") }},
		{"error", func(r *output.Renderer) { r.Error("hi") }},
	}
	for _, tc := range cases {
		t.Run(tc.name+" pretty", func(t *testing.T) {
			r, out, errb := newR(false)
			tc.call(r)
			if out.Len() != 0 {
				t.Errorf("chatter leaked to stdout: %q", out.String())
			}
			if !strings.Contains(errb.String(), "hi") {
				t.Errorf("want 'hi' on stderr; got %q", errb.String())
			}
		})
		t.Run(tc.name+" json", func(t *testing.T) {
			r, out, errb := newR(true)
			tc.call(r)
			if out.Len() != 0 || errb.Len() != 0 {
				t.Errorf("status helpers must be silent in --json; stdout=%q stderr=%q",
					out.String(), errb.String())
			}
		})
	}
}
