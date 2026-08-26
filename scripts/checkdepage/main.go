// Command checkdepage fails the build when a module in the build list was
// published more recently than the cooldown window. Feed it
// `go list -m -json all` on stdin.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type module struct {
	Path    string
	Version string
	Time    *time.Time
	Main    bool
	Replace *module
}

type finding struct {
	Path    string
	Version string
	Age     time.Duration
}

func findTooNew(r io.Reader, now time.Time, window time.Duration, ignore map[string]bool) ([]finding, error) {
	dec := json.NewDecoder(r)
	var out []finding
	for dec.More() {
		var m module
		if err := dec.Decode(&m); err != nil {
			return nil, err
		}
		eff := m
		if m.Replace != nil {
			eff = *m.Replace
		}
		if eff.Main || eff.Version == "" || eff.Time == nil || ignore[eff.Path] {
			continue
		}
		if age := now.Sub(*eff.Time); age < window {
			out = append(out, finding{eff.Path, eff.Version, age})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Age < out[j].Age })
	return out, nil
}

func main() {
	days := 7
	if v := os.Getenv("DEP_COOLDOWN_DAYS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			fmt.Fprintf(os.Stderr, "invalid DEP_COOLDOWN_DAYS %q (must be a positive integer)\n", v)
			os.Exit(2)
		}
		days = n
	}
	ignore := map[string]bool{}
	for _, p := range strings.Split(os.Getenv("DEP_COOLDOWN_IGNORE"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			ignore[p] = true
		}
	}

	window := time.Duration(days) * 24 * time.Hour
	found, err := findTooNew(os.Stdin, time.Now(), window, ignore)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parsing `go list -m -json all` output:", err)
		os.Exit(2)
	}
	if len(found) == 0 {
		fmt.Printf("dep-age: all dependencies are at least %d days old\n", days)
		return
	}
	for _, f := range found {
		fmt.Fprintf(os.Stderr, "  %s %s (published %.1f days ago)\n", f.Path, f.Version, f.Age.Hours()/24)
	}
	fmt.Fprintf(os.Stderr, "Wait for them to age out, or override a specific module with DEP_COOLDOWN_IGNORE=<module-path>.\n")
	fmt.Fprintf(os.Stderr, "::error::%d dependency version(s) younger than the %d-day supply-chain cooldown (see the list above)\n", len(found), days)
	os.Exit(1)
}
