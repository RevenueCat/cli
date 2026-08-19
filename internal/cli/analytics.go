package cli

import (
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// Usage-analytics request headers. The backend derives command usage and the
// humans-vs-agents split from these on normal request logs.
const (
	headerCLICommand = "X-RC-CLI-Command"
	headerCLIMode    = "X-RC-CLI-Mode"
)

// dottedCommandPath turns a cobra command path ("rc paywalls generate") into a
// dotted identifier ("paywalls.generate"). It is deliberately the command path
// only — never args, flag values, IDs, emails, or prompts — so the header can't
// carry user or account data. Returns "" for the bare root command.
func dottedCommandPath(cmd *cobra.Command) string {
	fields := strings.Fields(commandPath(cmd))
	if len(fields) <= 1 {
		return ""
	}
	return strings.Join(fields[1:], ".")
}

// cliMode reports how the CLI is being driven: "ci" under a CI environment,
// "agent" when output is machine-driven (--json / --no-input), else
// "interactive".
func cliMode(g *Globals) string {
	if os.Getenv("CI") != "" {
		return "ci"
	}
	if g.JSON || g.NoInput {
		return "agent"
	}
	return "interactive"
}

// doNotTrack honors the DO_NOT_TRACK convention: any truthy value opts out.
func doNotTrack() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DO_NOT_TRACK"))) {
	case "1", "true":
		return true
	}
	return false
}

// requestHeaders assembles the extra headers sent on every v2 API request: the
// usage-analytics headers (dropped when DO_NOT_TRACK is set — the request still
// goes through, just unlabeled) plus any user-supplied RC_HEADERS, which
// override so operators keep the final say.
func requestHeaders(g *Globals) http.Header {
	h := http.Header{}
	if !doNotTrack() {
		if g.CommandPath != "" {
			h.Set(headerCLICommand, g.CommandPath)
		}
		h.Set(headerCLIMode, cliMode(g))
	}
	for name, values := range customHeaders() {
		h[http.CanonicalHeaderKey(name)] = values
	}
	if len(h) == 0 {
		return nil
	}
	return h
}
