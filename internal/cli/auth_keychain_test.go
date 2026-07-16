package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveRevenueCatPasswordToKeychainUsesStdinAndWebsiteCredential(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "security")
	argsFile := filepath.Join(dir, "args")
	stdinFile := filepath.Join(dir, "stdin")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGS_FILE\"\ncat > \"$STDIN_FILE\"\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARGS_FILE", argsFile)
	t.Setenv("STDIN_FILE", stdinFile)

	const password = "secret-that-must-not-be-an-argument"
	if err := saveRevenueCatPasswordToKeychain(context.Background(), "dev@example.com", password, executable); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(args), password) {
		t.Fatal("password was exposed in process arguments")
	}
	for _, want := range []string{"add-internet-password", "dev@example.com", "app.revenuecat.com", "RevenueCat", "htps", "-U", "-w"} {
		if !strings.Contains(string(args), want) {
			t.Errorf("security arguments missing %q:\n%s", want, args)
		}
	}
	stdin, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(stdin) != password+"\n"+password+"\n" {
		t.Fatalf("security stdin = %q", stdin)
	}
}

func TestSaveRevenueCatPasswordToKeychainRejectsLineBreaks(t *testing.T) {
	err := saveRevenueCatPasswordToKeychain(context.Background(), "dev@example.com", "secret\npassword", "security")
	if err == nil || !strings.Contains(err.Error(), "line breaks") {
		t.Fatalf("error = %v", err)
	}
}
