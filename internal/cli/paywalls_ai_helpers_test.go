package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWithPaywallContext(t *testing.T) {
	if got := withPaywallContext("make it blue", ""); got != "make it blue" {
		t.Fatalf("empty context should pass the prompt through, got %q", got)
	}
	got := withPaywallContext("make it blue", "fitness app, premium tier")
	if !strings.Contains(got, "fitness app") || !strings.Contains(got, "make it blue") {
		t.Fatalf("context and direction should both survive: %q", got)
	}
	if strings.Index(got, "fitness app") > strings.Index(got, "make it blue") {
		t.Fatalf("context should precede the direction: %q", got)
	}
}

func TestLoadPaywallAIAttachments(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(png, []byte("\x89PNG\r\n\x1a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	md := filepath.Join(dir, "DESIGN.md")
	if err := os.WriteFile(md, []byte("# Brand\nUse #0E1B2A"), 0o600); err != nil {
		t.Fatal(err)
	}

	imgs, textRefs, err := loadPaywallAIAttachments(nil, []string{png, md})
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != 1 {
		t.Fatalf("png should route to an image attachment, got %d", len(imgs))
	}
	if !strings.Contains(textRefs, "DESIGN.md") || !strings.Contains(textRefs, "#0E1B2A") {
		t.Fatalf("text attachment should fold into the direction: %q", textRefs)
	}

	if _, _, err := loadPaywallAIAttachments(nil, []string{filepath.Join(dir, "font.ttf")}); err == nil {
		t.Fatal("unsupported attachment type should error")
	}
}
