// Command scrub reads raw RevenueCat API responses from -in and writes
// scrubbed versions to -out, replacing identifying values with deterministic
// fakes so the result can be committed as test fixtures.
//
//	go run ./internal/api/testdata/scrub -in /tmp/rc-fixtures -out internal/api/testdata/v2
//
// Rules:
//   - String values matching known ID prefixes are mapped to a deterministic
//     fake (proj81507275 -> proj_test_001) preserving the prefix.
//   - Email-shaped strings become user-NNN@example.com.
//   - IPv4 addresses become 192.0.2.NNN (RFC 5737 test range).
//   - Bearer tokens / api keys (sk_*) are stripped entirely (set to "").
//   - Unix-millisecond timestamps (large integers under known keys) are
//     remapped to a reference epoch (2025-01-01) plus a small ascending offset.
//   - URLs are rewritten to use the scrubbed IDs.
//   - PII fields (name, label, user_agent) are replaced with stable fakes.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	inDir  = flag.String("in", "/tmp/rc-fixtures", "directory of raw fixture JSON files")
	outDir = flag.String("out", "internal/api/testdata/v2", "directory to write scrubbed fixtures")
	dryRun = flag.Bool("dry-run", false, "print scrubbed JSON to stdout instead of writing")
)

// Known RC ID prefixes. Order matters: longer prefixes must come before shorter ones.
var idPrefixes = []string{
	"apikey", "ofrng", "prod", "appa", "appe", "app", "proj", "cus_", "pw", "log",
}

// Field names whose string values look like sensitive PII regardless of shape.
var piiFields = map[string]string{
	"email":      "user@example.com",
	"name":       "Test User",
	"label":      "Test Label",
	"user_agent": "test-user-agent/1.0",
	"ip_address": "192.0.2.1",
}

// Field names that hold unix-millisecond timestamps we want stabilized.
var tsFields = map[string]struct{}{
	"created_at": {}, "first_seen_at": {}, "last_seen_at": {},
	"published_at": {}, "occurred_at": {}, "expires_at": {}, "granted_at": {},
}

var (
	emailRe = regexp.MustCompile(`(?i)[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}`)
	ipv4Re  = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	skRe    = regexp.MustCompile(`sk_[A-Za-z0-9]+`)
)

type scrubber struct {
	idMap    map[string]string
	idCount  map[string]int
	emailMap map[string]string
	ipMap    map[string]string
	tsBase   int64
	tsCount  int
}

func newScrubber() *scrubber {
	return &scrubber{
		idMap:    map[string]string{},
		idCount:  map[string]int{},
		emailMap: map[string]string{},
		ipMap:    map[string]string{},
		tsBase:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli(),
	}
}

func (s *scrubber) mapID(original string) string {
	if v, ok := s.idMap[original]; ok {
		return v
	}
	prefix := ""
	for _, p := range idPrefixes {
		if strings.HasPrefix(original, p) {
			prefix = p
			break
		}
	}
	if prefix == "" {
		// Heuristic: if it looks like alphanumeric blob >= 10 chars, scrub anyway.
		if len(original) >= 10 && isAlnum(original) {
			prefix = "id"
		} else {
			return original
		}
	}
	s.idCount[prefix]++
	fake := fmt.Sprintf("%s_test_%03d", strings.TrimRight(prefix, "_"), s.idCount[prefix])
	s.idMap[original] = fake
	return fake
}

func (s *scrubber) mapEmail(e string) string {
	if v, ok := s.emailMap[e]; ok {
		return v
	}
	v := fmt.Sprintf("user-%03d@example.com", len(s.emailMap)+1)
	s.emailMap[e] = v
	return v
}

func (s *scrubber) mapIP(ip string) string {
	if v, ok := s.ipMap[ip]; ok {
		return v
	}
	v := fmt.Sprintf("192.0.2.%d", len(s.ipMap)+1)
	s.ipMap[ip] = v
	return v
}

func (s *scrubber) mapTimestamp() int64 {
	s.tsCount++
	return s.tsBase + int64(s.tsCount*1000)
}

func isAlnum(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// scrubString applies regex-based substitutions (email, IP, sk_ tokens).
func (s *scrubber) scrubString(in string) string {
	out := skRe.ReplaceAllString(in, "")
	out = emailRe.ReplaceAllStringFunc(out, func(m string) string { return s.mapEmail(m) })
	out = ipv4Re.ReplaceAllStringFunc(out, func(m string) string {
		// avoid clobbering version strings like "Version 18.2"
		if parsed := net.ParseIP(m); parsed == nil || parsed.To4() == nil {
			return m
		}
		return s.mapIP(m)
	})
	// Map known IDs that appear embedded in URLs.
	for orig, fake := range s.idMap {
		out = strings.ReplaceAll(out, orig, fake)
	}
	return out
}

// walk recursively scrubs an arbitrary JSON value.
func (s *scrubber) walk(v any, parentKey string) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		// First pass: scrub `id` fields up-front so URLs and *_id references
		// in the same object resolve to the same fake.
		idHandled := map[string]bool{}
		for _, k := range keys {
			if k == "id" {
				if s2, ok := t[k].(string); ok {
					t[k] = s.mapID(s2)
					idHandled[k] = true
				}
			}
		}
		for _, k := range keys {
			if idHandled[k] {
				continue
			}
			val := t[k]
			if _, isPII := piiFields[k]; isPII {
				if str, ok := val.(string); ok && str != "" {
					t[k] = piiFields[k]
					continue
				}
			}
			if _, isTS := tsFields[k]; isTS {
				if num, ok := val.(float64); ok && num > 1e12 {
					t[k] = s.mapTimestamp()
					continue
				}
			}
			t[k] = s.walk(val, k)
		}
		return t
	case []any:
		for i := range t {
			t[i] = s.walk(t[i], parentKey)
		}
		return t
	case string:
		// Field-level ID rewrites for *_id keys and known prefixes.
		if strings.HasSuffix(parentKey, "_id") || parentKey == "id" {
			return s.mapID(t)
		}
		return s.scrubString(t)
	default:
		return t
	}
}

func main() {
	flag.Parse()
	entries, err := os.ReadDir(*inDir)
	must(err)

	s := newScrubber()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(*inDir, e.Name()))
		must(err)
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", e.Name(), err)
			continue
		}
		v = s.walk(v, "")
		out, err := json.MarshalIndent(v, "", "  ")
		must(err)
		out = append(out, '\n')
		if *dryRun {
			fmt.Printf("=== %s ===\n%s\n", e.Name(), out)
			continue
		}
		must(os.MkdirAll(*outDir, 0o755))
		dst := filepath.Join(*outDir, e.Name())
		must(os.WriteFile(dst, out, 0o644))
		fmt.Println("wrote", dst)
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
