package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"plain-id_1.2":                   "plain-id_1.2",
		"keep\nnewlines\tand tabs":       "keep\nnewlines\tand tabs",
		"esc\x1b]52;c;UkNCQg==\x07seq":   "esc]52;c;UkNCQg==seq",
		"csi\x1b[31mred\x1b[0m":          "csi[31mred[0m",
		"bell\x07 backspace\x08 del\x7f": "bell backspace del",
		"c1\u0085dev\u009bice\u0090str":  "c1devicestr",
		"unicode üñî remains":            "unicode üñî remains",
		"\x00\x01\x02only-controls\x1f":  "only-controls",
		"carriage\rreturn":               "carriagereturn",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderTable_ValuesRenderAsVisibleText(t *testing.T) {
	var out, errBuf bytes.Buffer
	r := NewRenderer(&out, &errBuf, false, true, false, "")
	err := r.RenderTable(Table{
		Columns: []string{"ID", "NAME"},
		Rows: [][]string{
			{"cus_\x1b]52;c;UkNCQg==\x07x", "a\rb"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s := out.String(); strings.ContainsAny(s, "\x1b\x07\r") {
		t.Errorf("table output contains raw control bytes: %q", s)
	}
}

func TestRenderHuman_ValuesRenderAsVisibleText(t *testing.T) {
	var out, errBuf bytes.Buffer
	r := NewRenderer(&out, &errBuf, false, true, false, "")
	err := r.Render(map[string]any{
		"id":           "cus_\x1b]52;c;UkNCQg==\x07x",
		"na\x1bme_key": "value\x07",
	})
	if err != nil {
		t.Fatal(err)
	}
	if s := out.String(); strings.ContainsAny(s, "\x1b\x07") {
		t.Errorf("human output contains raw control bytes: %q", s)
	}
}

func TestRenderJSON_LeavesValuesEncoded(t *testing.T) {
	var out, errBuf bytes.Buffer
	r := NewRenderer(&out, &errBuf, true, true, false, "")
	if err := r.Render(map[string]any{"id": "a\x1bb"}); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if strings.Contains(s, "\x1b") {
		t.Errorf("json output contains a raw escape byte: %q", s)
	}
	if !strings.Contains(s, `\u001b`) {
		t.Errorf("json output should keep the value losslessly escaped, got %q", s)
	}
}
