package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/revenuecat/cli/internal/output"
)

type homeGroup struct {
	title string
	rows  [][2]string // {command, description}
}

// homeMap is the curated command catalog shown once authenticated — a
// discoverable slice of the surface, not the full tree (that's `rc --help`).
var homeMap = []homeGroup{
	{"Build", [][2]string{
		{"rc paywalls", "design a paywall with AI ✨"},
		{"rc offerings list", "offerings & their packages"},
		{"rc products list", "products across your stores"},
	}},
	{"Inspect", [][2]string{
		{"rc customer show <id>", "a full customer view"},
		{"rc charts show mrr", "MRR, actives, conversion"},
		{"rc metrics", "project overview at a glance"},
	}},
	{"Automate & set up", [][2]string{
		{"rc apps", "connect App Store & Google Play"},
		{"rc skills install", "AI workflows for coding agents"},
		{"rc rico chat", "ask RevenueCat's AI ✨"},
	}},
}

// writeHomeScreen renders the bare-`rc` landing screen. It reads only local
// config — never the network — so bare `rc` stays instant, and tailors itself
// to where the user is: setup steps when logged out, a project nudge and the
// full command map once authenticated.
func writeHomeScreen(w io.Writer, rt *Runtime) {
	paint := rt.Out.Paint
	label := func(s string) string { return paint(output.ToneTitle, s) }

	var b strings.Builder
	b.WriteString(homePanel(rt) + "\n\n")

	authed := rt.Config.BearerToken() != ""
	switch {
	case !authed:
		b.WriteString(label("New here? Two steps to get going:") + "\n")
		b.WriteString("  " + paint(output.ToneCommand, "1  rc auth login") + "     " + paint(output.ToneDim, "log in (browser or API key)") + "\n")
		b.WriteString("  " + paint(output.ToneCommand, "2  rc projects use") + "   " + paint(output.ToneDim, "pick a project") + "\n")

	default:
		if rt.Config.ProjectID == "" {
			b.WriteString(label("Pick a project so commands know where to run:") + "\n")
			b.WriteString(homeRows(paint, [][2]string{{"rc projects use", "choose a default project"}}, 0))
			b.WriteString("\n")
		}
		mapWidth := 0
		for _, g := range homeMap {
			for _, r := range g.rows {
				if len(r[0]) > mapWidth {
					mapWidth = len(r[0])
				}
			}
		}
		for i, g := range homeMap {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(label(g.title) + "\n")
			b.WriteString(homeRows(paint, g.rows, mapWidth))
		}
	}

	b.WriteString("\n" + paint(output.ToneDim, "rc --help") + " for everything · " +
		paint(output.ToneDim, "rc <command> --help") + " for details\n")
	fmt.Fprint(w, b.String())
}

// homePanel is the boxed status header: identity, project, and a dashboard
// deep-link when they're known.
func homePanel(rt *Runtime) string {
	paint := rt.Out.Paint
	lines := []string{paint(output.ToneAccent, "rc · RevenueCat")}

	if rt.Config.BearerToken() == "" {
		lines = append(lines, paint(output.ToneDim, "not logged in"))
		return rt.Out.Panel(lines...)
	}

	identity := accountIdentity(rt)
	if identity == "" {
		identity = "logged in"
	}
	lines = append(lines, paint(output.ToneSuccess, "✓ ")+identity)

	if rt.Config.ProjectID != "" {
		lines = append(lines,
			paint(output.ToneDim, "project "+rt.Config.ProjectID),
			paint(output.ToneLink, "https://app.revenuecat.com/projects/"+dashboardProjectID(rt.Config.ProjectID)))
	} else {
		lines = append(lines, paint(output.ToneDim, "no project selected"))
	}
	return rt.Out.Panel(lines...)
}

func accountIdentity(rt *Runtime) string {
	if id := rt.Config.AccountEmail; id != "" {
		return id
	}
	return rt.Config.AccountName
}

// homeRows renders command/description pairs as an aligned two-column block:
// the command in the interaction accent (with <placeholders> dimmed so they
// read as fill-ins), the description dimmed. Pass width to align the
// description column across several blocks; 0 fits each block on its own.
func homeRows(paint func(output.Tone, string) string, rows [][2]string, width int) string {
	for _, r := range rows {
		if len(r[0]) > width {
			width = len(r[0])
		}
	}
	var b strings.Builder
	for _, r := range rows {
		pad := strings.Repeat(" ", width-len(r[0]))
		b.WriteString("  " + paintCommand(paint, r[0]) + pad + "  " + paint(output.ToneDim, r[1]) + "\n")
	}
	return b.String()
}

// paintCommand colors a command string, dimming any <...> placeholder segments.
func paintCommand(paint func(output.Tone, string) string, s string) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(s, '<')
		if i < 0 {
			b.WriteString(paint(output.ToneCommand, s))
			return b.String()
		}
		j := strings.IndexByte(s[i:], '>')
		if j < 0 {
			b.WriteString(paint(output.ToneCommand, s))
			return b.String()
		}
		j += i
		b.WriteString(paint(output.ToneCommand, s[:i]))
		b.WriteString(paint(output.ToneDim, s[i:j+1]))
		s = s[j+1:]
	}
}
