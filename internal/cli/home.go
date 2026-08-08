package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/revenuecat/cli/internal/output"
)

// writeHomeScreen renders the bare-`rc` landing screen. It reads only local
// config — never the network — so bare `rc` stays instant, and tailors itself
// to where the user is: setup steps when logged out, things to actually do
// once authenticated.
func writeHomeScreen(w io.Writer, rt *Runtime) {
	paint := rt.Out.Paint
	cmd := func(s string) string { return paint(output.ToneCommand, s) }
	desc := func(s string) string { return paint(output.ToneDim, s) }
	label := func(s string) string { return paint(output.ToneTitle, s) }

	var b strings.Builder
	b.WriteString(paint(output.ToneAccent, "▍ rc") + desc("  the RevenueCat CLI") + "\n\n")

	more := "\n" + label("More:") + "\n" + homeActions(paint, [][2]string{
		{"rc --help", "every command"},
		{"rc <command> --help", "details for any command"},
	})

	authed := rt.Config.BearerToken() != ""
	switch {
	case !authed:
		b.WriteString(label("New here? Two steps to get going:") + "\n")
		b.WriteString("  " + cmd("1") + "  " + cmd("rc auth login") + "     " + desc("log in (browser or API key)") + "\n")
		b.WriteString("  " + cmd("2") + "  " + cmd("rc projects use") + "   " + desc("pick a project") + "\n\n")
		b.WriteString(label("Then try:") + "\n")
		b.WriteString(homeActions(paint, exploreActions))

	case rt.Config.ProjectID == "":
		b.WriteString(paint(output.ToneSuccess, "✓ ") + loggedInAs(rt) + "\n\n")
		b.WriteString(label("Next — pick a project so commands know where to run:") + "\n")
		b.WriteString(homeActions(paint, [][2]string{{"rc projects use", "choose a default project"}}))
		b.WriteString("\n" + label("Then:") + "\n")
		b.WriteString(homeActions(paint, exploreActions))

	default:
		b.WriteString(paint(output.ToneSuccess, "✓ ") + loggedInAs(rt) +
			desc(" · project "+rt.Config.ProjectID) + "\n\n")
		b.WriteString(label("Do something:") + "\n")
		b.WriteString(homeActions(paint, exploreActions))
	}

	b.WriteString(more)
	fmt.Fprint(w, b.String())
}

var exploreActions = [][2]string{
	{"rc paywalls", "design a paywall with AI"},
	{"rc customer show <id>", "look up a customer"},
	{"rc charts show mrr", "see your MRR"},
}

// loggedInAs is the identity line: cached email/name when known, otherwise a
// plain confirmation (an API-key login carries no cached identity).
func loggedInAs(rt *Runtime) string {
	if id := rt.Config.AccountEmail; id != "" {
		return "Logged in as " + id
	}
	if id := rt.Config.AccountName; id != "" {
		return "Logged in as " + id
	}
	return "Logged in"
}

// homeActions renders command/description pairs as an aligned two-column
// block: the command in the interaction accent, the description dimmed.
func homeActions(paint func(output.Tone, string) string, rows [][2]string) string {
	width := 0
	for _, r := range rows {
		if len(r[0]) > width {
			width = len(r[0])
		}
	}
	var b strings.Builder
	for _, r := range rows {
		pad := strings.Repeat(" ", width-len(r[0]))
		b.WriteString("  " + paint(output.ToneCommand, r[0]) + pad + "  " + paint(output.ToneDim, r[1]) + "\n")
	}
	return b.String()
}
