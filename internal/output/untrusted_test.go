package output_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/revenuecat/cli/internal/output"
)

// osc52 asks the terminal to replace the clipboard with "RCBB191". An App User
// ID is enough to carry it, so it stands in here for any remote string.
const osc52 = "rcbb_target\x1b]52;c;UkNCQjE5MQ==\x07"

func TestSanitize_NeutralizesControlsAndLeavesTextAlone(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain text untouched", "cus_abc123", "cus_abc123"},
		{"unicode untouched", "日本語 café 🐈", "日本語 café 🐈"},
		{"newline and tab survive", "line\nnext\tcell", "line\nnext\tcell"},
		{"clipboard write", osc52, `rcbb_target\x1b]52;c;UkNCQjE5MQ==\x07`},
		{"cursor movement", "a\x1b[2Jb", `a\x1b[2Jb`},
		{"carriage return overwrite", "real\rfake", `real\x0dfake`},
		{"del", "a\x7fb", `a\x7fb`},
		{"c1 csi", "a\u009bb", `a\u009bb`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := output.Sanitize(tc.in); got != tc.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRenderTable_EscapesControlSequencesAndKeepsAlignment(t *testing.T) {
	r, out, _ := newR(false)
	err := r.RenderTable(output.Table{
		Columns: []string{"ID", "PLATFORM"},
		Rows:    [][]string{{osc52, "ios"}, {"cus_plain", "android"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertNoEscapes(t, "table stdout", out.String())
	if !strings.Contains(out.String(), `\x1b]52`) {
		t.Errorf("expected the sequence shown as an escaped literal:\n%q", out.String())
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	for i, l := range lines[1:] {
		if len(l) != len(lines[0]) {
			t.Errorf("row %d width %d != header width %d (escaping must be included in the width)", i+1, len(l), len(lines[0]))
		}
	}
}

// Escaping is not a side effect of --no-color: the colored path styles the
// value after it has been neutralized.
func TestRenderTable_EscapesControlSequencesWithColorEnabled(t *testing.T) {
	var out, errb strings.Builder
	r := output.NewRenderer(&out, &errb, false, false, false, "")
	err := r.RenderTable(output.Table{Columns: []string{"ID"}, Rows: [][]string{{osc52}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b]52") || strings.ContainsRune(out.String(), 0x07) {
		t.Errorf("colored table leaked the clipboard sequence:\n%q", out.String())
	}
}

func TestRenderCard_EscapesControlSequencesInEverySlot(t *testing.T) {
	r, out, _ := newR(false)
	err := r.RenderCard(output.Card{
		Title:    osc52,
		Subtitle: osc52,
		Sections: []output.CardSection{
			{Heading: "Active entitlements", Chips: []output.Chip{{Label: osc52}}},
			{Heading: "Subscriptions", Table: &output.CardTable{Columns: []string{"ID"}, Rows: [][]string{{osc52}}}},
			{Heading: "Attributes", Lines: []output.CardLine{{Key: osc52, Value: osc52}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertNoEscapes(t, "card stdout", out.String())
}

func TestRenderHuman_EscapesControlSequencesInKeysAndValues(t *testing.T) {
	r, out, _ := newR(false)
	if err := r.Render(map[string]string{"id": osc52, osc52: "value"}); err != nil {
		t.Fatal(err)
	}
	assertNoEscapes(t, "human stdout", out.String())
}

func TestChatter_EscapesControlSequences(t *testing.T) {
	cases := map[string]func(r *output.Renderer){
		"success": func(r *output.Renderer) { r.Success(osc52) },
		"info":    func(r *output.Renderer) { r.Info(osc52) },
		"warn":    func(r *output.Renderer) { r.Warn(osc52) },
		"always":  func(r *output.Renderer) { r.AlwaysWarn(osc52) },
		"error":   func(r *output.Renderer) { r.Error(osc52) },
		"hint":    func(r *output.Renderer) { r.Hint(osc52) },
		"title":   func(r *output.Renderer) { r.Title(osc52) },
		"lead":    func(r *output.Renderer) { r.Lead(osc52) },
		"notice":  func(r *output.Renderer) { r.Notice(osc52) },
		"answer":  func(r *output.Renderer) { r.Answer("Customer", osc52) },
		"field":   func(r *output.Renderer) { r.Field("Customer", osc52, osc52) },
		"plan":    func(r *output.Renderer) { r.Plan([]string{osc52}) },
		"link":    func(r *output.Renderer) { r.LinkLine("https://example.com/" + osc52) },
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			r, out, errb := newR(false)
			call(r)
			assertNoEscapes(t, name+" stderr", errb.String())
			assertNoEscapes(t, name+" stdout", out.String())
		})
	}
}

// --json is the agent contract and must keep carrying the exact bytes, safely:
// the JSON encoder escapes them, so nothing reaches the terminal as a sequence.
func TestRenderJSON_KeepsControlBytesEncodedNotExecuted(t *testing.T) {
	r, out, _ := newR(true)
	if err := r.Render(map[string]string{"id": osc52}); err != nil {
		t.Fatal(err)
	}
	assertNoEscapes(t, "json stdout", out.String())
	var got struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Data["id"] != osc52 {
		t.Errorf("--json must round-trip the raw value; got %q", got.Data["id"])
	}
}

func assertNoEscapes(t *testing.T, where, s string) {
	t.Helper()
	if strings.ContainsRune(s, 0x1b) {
		t.Errorf("%s carries a raw escape byte a terminal would act on:\n%q", where, s)
	}
	if strings.ContainsRune(s, 0x07) {
		t.Errorf("%s carries a raw BEL:\n%q", where, s)
	}
}
