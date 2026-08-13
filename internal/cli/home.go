package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/revenuecat/cli/internal/output"
)

type homeGroup struct {
	groupID string      // key into commandGroups; title comes from there
	rows    [][2]string // {command, description}
}

// homeMap is the curated command catalog shown once authenticated. Groups key
// into commandGroups so titles and order match `rc --help`.
var homeMap = []homeGroup{
	{"start", [][2]string{
		{"rc setup", "set up RevenueCat for a new app"},
	}},
	{"design", [][2]string{
		{"rc paywalls generate", "design a paywall with AI"},
		{"rc paywalls edit", "refine an existing paywall"},
	}},
	{"catalog", [][2]string{
		{"rc offerings list", "offerings & their packages"},
		{"rc products list", "products across your stores"},
		{"rc entitlements list", "entitlements & their products"},
	}},
	{"revenue", [][2]string{
		{"rc customers show <id>", "a full customer view"},
		{"rc charts show mrr", "MRR, actives, conversion"},
		{"rc metrics", "project overview at a glance"},
	}},
	{"integrations", [][2]string{
		{"rc apps", "connect App Store & Google Play"},
	}},
	{"ai", [][2]string{
		{"rc skills install", "AI workflows for coding agents"},
		{"rc rico", "ask RevenueCat's AI ✨"},
	}},
}

// writeHomeScreen renders the bare-`rc` landing screen from local config only.
func writeHomeScreen(w io.Writer, rt *Runtime) {
	paint := rt.Out.Paint
	label := func(s string) string { return paint(output.ToneTitle, s) }

	var b strings.Builder
	b.WriteString(homePanel(rt) + "\n\n")

	authed := rt.Config.BearerToken() != ""
	switch {
	case !authed:
		b.WriteString(label("New here? One command sets everything up:") + "\n")
		b.WriteString("  " + paint(output.ToneCommand, "rc setup") + "   " + paint(output.ToneDim, "an AI agent sets up RevenueCat for this app (you approve each step)") + "\n\n")
		b.WriteString(label("Prefer to wire it up yourself?") + "\n")
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
			b.WriteString(label(groupTitle(g.groupID)) + "\n")
			b.WriteString(homeRows(paint, g.rows, mapWidth))
		}
	}

	b.WriteString("\n" + paint(output.ToneDim, "rc --help") + " for everything · " +
		paint(output.ToneDim, "rc <command> --help") + " for details\n")
	fmt.Fprint(w, b.String())
}

// homePanel is the boxed status header: identity, project, dashboard link.
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

// homeRows renders command/description pairs as an aligned two-column block.
// width aligns the description column across blocks; 0 fits each on its own.
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
