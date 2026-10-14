package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode"
)

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"plain-id_1.2":                   "plain-id_1.2",
		"keep\nnewlines\tand tabs":       "keep\nnewlines\tand tabs",
		"osc\x1b]0;title\x07seq":         "osc]0;titleseq",
		"csi\x1b[31mred\x1b[0m":          "csi[31mred[0m",
		"bell\x07 backspace\x08 del\x7f": "bell backspace del",
		"c1\u0085dev\u009bice\u0090str":  "c1devicestr",
		"unicode üñî remains":            "unicode üñî remains",
		"\x00\x01\x02only-controls\x1f":  "only-controls",
		"carriage\rreturn":               "carriagereturn",
		// Bidi overrides and isolates are invisible but reverse the glyphs
		// around them, so the rendered text reads differently from the value.
		"Premium \u202eEVIL":          "Premium EVIL",
		"\u2066admin\u2069":           "admin",
		"\u202dltr\u202c":             "ltr",
		"\u061cmark":                  "mark",
		"row1\u2028row2\u2029x":       "row1row2x",
		"left\u200emiddle\u200fright": "leftmiddleright",
		"ad\u200bmin":                 `ad\u200bmin`,
		"no\u2060break":               `no\u2060break`,
		"bom\ufeffhere":               `bom\ufeffhere`,
		"soft\u00adhyphen":            `soft\u00adhyphen`,
		"tag\U000e0001here":           `tag\U000e0001here`,
		// Join controls and variation selectors preserve shaping and emoji.
		"family 👨\u200d👩\u200d👧": "family 👨\u200d👩\u200d👧",
		"zwnj \u200chere":        "zwnj \u200chere",
		"می\u200cروم ❤\ufe0f":    "می\u200cروم ❤\ufe0f",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitize_AllBidiAndFormatCharacters(t *testing.T) {
	var input strings.Builder
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if unicode.Is(unicode.Cf, r) {
			input.WriteRune(r)
		}
	}
	for name, sanitize := range map[string]func(string) string{"multiline": Sanitize, "line": SanitizeLine} {
		t.Run(name, func(t *testing.T) {
			got := sanitize(input.String())
			for _, r := range got {
				if unicode.Is(unicode.Bidi_Control, r) || unicode.Is(unicode.Cf, r) && !unicode.Is(unicode.Join_Control, r) {
					t.Errorf("unsafe format character U+%04X survived", r)
				}
			}
			if !strings.ContainsRune(got, '\u200c') || !strings.ContainsRune(got, '\u200d') {
				t.Fatal("join controls required for shaping were lost")
			}
			for _, r := range []rune{'\u061c', '\u200e', '\u200f', '\u202e', '\u2066', '\u2069'} {
				if text := sanitize("left" + string(r) + "right"); text != "leftright" {
					t.Errorf("bidi control U+%04X was not removed: %q", r, text)
				}
			}
			if again := sanitize(got); again != got {
				t.Fatal("sanitizing visible escapes twice changed the text")
			}
		})
	}
}

func TestEscapeJSON_PreservesUnicodeValues(t *testing.T) {
	const value = "a\u0085\u202e\u200e\u200f\u200b\u2060\ufeff\u2028\u2029\U000e0001👨\u200d👩می\u200cروم❤\ufe0f"
	want := map[string]string{value: value, "literal": `\u202e`}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	got := EscapeJSON(raw)
	for _, r := range string(got) {
		if r >= 0x80 && r <= 0x9f || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			t.Errorf("JSON emitted raw U+%04X", r)
		}
	}
	if !bytes.Contains(got, []byte(`\udb40\udc01`)) {
		t.Fatal("supplementary format character must use a JSON surrogate pair")
	}
	var decoded map[string]string
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("JSON round trip changed values: %q", decoded)
	}
	if !bytes.Equal(got, EscapeJSON(got)) {
		t.Fatal("escaping JSON twice changed it")
	}

	// Raw API responses can contain separators that encoding/json escapes.
	raw = []byte(fmt.Sprintf("{\"value\":\"%s\"}", value))
	if err := json.Unmarshal(EscapeJSON(raw), &decoded); err != nil || decoded["value"] != value {
		t.Fatalf("raw API JSON did not round trip: decoded=%q err=%v", decoded, err)
	}
}

func TestSanitizeLine(t *testing.T) {
	cases := map[string]string{
		"one line":                  "one line",
		"two\nlines\tand tab":       "two lines and tab",
		"ctrl\x1b[2Jhere":           "ctrl[2Jhere",
		"Premium \u202eEVIL":        "Premium EVIL",
		"row1\u2028row2\u2029x":     "row1 row2 x",
		"family 👨\u200d👩":           "family 👨\u200d👩",
		"ad\u200bmin no\u2060break": `ad\u200bmin no\u2060break`,
		"left\u200eright\u200f":     "leftright",
	}
	for in, want := range cases {
		if got := SanitizeLine(in); got != want {
			t.Errorf("SanitizeLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderTable_ValuesRenderAsVisibleText(t *testing.T) {
	var out, errBuf bytes.Buffer
	r := NewRenderer(&out, &errBuf, false, true, false, "")
	err := r.RenderTable(Table{
		Columns: []string{"ID", "NAME"},
		Rows: [][]string{
			{"id_\x1b]0;title\x07x", "a\rb\nc"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s := out.String(); strings.ContainsAny(s, "\x1b\x07\r") {
		t.Errorf("table output contains raw control bytes: %q", s)
	}
}

func TestRenderCard_ValuesRenderAsVisibleText(t *testing.T) {
	var out, errBuf bytes.Buffer
	r := NewRenderer(&out, &errBuf, false, true, false, "")
	err := r.RenderCard(Card{
		Title:    "id_\x1b]0;title\x07x",
		Subtitle: "sub\x1b[2J",
		Sections: []CardSection{
			{Heading: "chips\x07", Chips: []Chip{{Label: "ent\x1b[31m"}}},
			{Heading: "table", Table: &CardTable{Columns: []string{"A"}, Rows: [][]string{{"v\x1b[2J"}}}},
			{Heading: "lines", Lines: []CardLine{{Key: "k\x1b", Value: "v\x07"}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if s := out.String(); strings.ContainsAny(s, "\x1b\x07") {
		t.Errorf("card output contains raw control bytes: %q", s)
	}
}

func TestRenderHuman_ValuesRenderAsVisibleText(t *testing.T) {
	var out, errBuf bytes.Buffer
	r := NewRenderer(&out, &errBuf, false, true, false, "")
	err := r.Render(map[string]any{
		"id":           "id_\x1b]0;title\x07x",
		"na\x1bme_key": "value\x07",
	})
	if err != nil {
		t.Fatal(err)
	}
	if s := out.String(); strings.ContainsAny(s, "\x1b\x07") {
		t.Errorf("human output contains raw control bytes: %q", s)
	}
}

// JSON output must stay losslessly escaped rather than stripped — including C1
// codepoints, which encoding/json would otherwise emit as raw UTF-8 bytes.
func TestRenderJSON_LeavesValuesEncoded(t *testing.T) {
	var out, errBuf bytes.Buffer
	r := NewRenderer(&out, &errBuf, true, true, false, "")
	if err := r.Render(map[string]any{"id": "a\x1bb\u0085c"}); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if strings.Contains(s, "\x1b") || strings.Contains(s, "\u0085") {
		t.Errorf("json output contains a raw control byte: %q", s)
	}
	if !strings.Contains(s, `\u001b`) || !strings.Contains(s, `\u0085`) {
		t.Errorf("json output should keep the value losslessly escaped, got %q", s)
	}
}

func TestHyperlink_URLCannotTerminateSequence(t *testing.T) {
	got := Hyperlink("label", "https://example.com/\x1b\\x\x07a\nb")
	if strings.Count(got, "\x1b]8;;") != 2 || strings.ContainsAny(got, "\x07\n") {
		t.Errorf("hyperlink URL broke out of the OSC 8 sequence: %q", got)
	}
}

// Link uses the URL as its own label, and Hyperlink can only sanitize the
// target it wraps — so Link has to clean the URL before it becomes a label.
func TestLink_LabelCannotCarryControlBytes(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	var out, errBuf bytes.Buffer
	for _, noColor := range []bool{true, false} {
		r := NewRenderer(&out, &errBuf, false, noColor, false, "")
		got := r.Link("https://example.com/\x1b]52;c;cGF5bG9hZA==\x07x")
		if strings.Contains(got, "\x07") {
			t.Errorf("noColor=%v: link label carried a control byte: %q", noColor, got)
		}
		if noColor && strings.Contains(got, "\x1b") {
			t.Errorf("no-color link emitted an escape: %q", got)
		}
		if !noColor && strings.Count(got, "\x1b]8;;") != 2 {
			t.Errorf("link broke out of the OSC 8 sequence: %q", got)
		}
	}
}

// The note is styled after sanitizing; sanitizing the composed string would
// strip the styling itself.
func TestField_SanitizesValueAndNoteSeparately(t *testing.T) {
	var out, errBuf bytes.Buffer
	r := NewRenderer(&out, &errBuf, false, true, false, "")
	r.Field("k\x1bey", "va\x07lue", "no\x1b[2Jte")
	s := errBuf.String()
	if strings.ContainsAny(s, "\x1b\x07") {
		t.Errorf("field output contains raw control bytes: %q", s)
	}
	if !strings.Contains(s, "value") || !strings.Contains(s, "no[2Jte") {
		t.Errorf("field output lost its text: %q", s)
	}
}

func TestRenderFormat_EscapesControlBytes(t *testing.T) {
	var out, errBuf bytes.Buffer
	r := NewRenderer(&out, &errBuf, true, true, false, ".data.items[]")
	err := r.Render(map[string]any{"items": []any{
		"str\x1bing\u0085val",
		map[string]any{"id": "ob\u0085ject"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if strings.Contains(s, "\x1b") || strings.Contains(s, "\u0085") {
		t.Errorf("--format output contains raw control bytes: %q", s)
	}
	if !strings.Contains(s, `\u0085`) {
		t.Errorf("--format JSON results should keep C1 escaped, got %q", s)
	}
}
