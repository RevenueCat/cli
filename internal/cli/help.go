package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/revenuecat/cli/internal/output"
)

// helpColor reflects whether help output should be styled (driven by the
// --no-color flag). Cobra's template funcs are process-global, so this is a
// package var the root help func sets just before each render; lipgloss then
// drops styling on non-TTY output and when NO_COLOR is set.
var helpColor = true

// rcUsageTemplate is cobra's default usage template with two changes: section
// headers and command names are styled, and the inherited-flag block — which
// cobra repeats in full on every subcommand — is collapsed to a one-liner by
// rcGlobalFlags.
const rcUsageTemplate = `{{rcHead "Usage:"}}{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

{{rcHead "Aliases:"}}
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

{{rcHead "Examples:"}}
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

{{$cmds := .Commands}}{{if eq (len .Groups) 0}}{{rcHead "Available Commands:"}}{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rcCmd (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}{{rcHead $group.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rcCmd (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}

{{end}}{{if not .AllChildCommandsHaveGroup}}{{rcHead "Additional Commands:"}}{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rcCmd (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{rcHead "Flags:"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

{{rcGlobalFlags .}}{{end}}{{if .HasHelpSubCommands}}

{{rcHead "Additional help topics:"}}{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rcCmd (rpad .CommandPath .CommandPathPadding)}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

{{rcFoot (printf "Use \"%s [command] --help\" for more information about a command." .CommandPath)}}{{end}}
`

// commandGroups define the sections of `rc --help`, in display order.
var commandGroups = []*cobra.Group{
	{ID: "start", Title: "Getting started"},
	{ID: "design", Title: "Paywalls & design"},
	{ID: "catalog", Title: "Catalog"},
	{ID: "revenue", Title: "Customers & revenue"},
	{ID: "integrations", Title: "Apps & integrations"},
	{ID: "ai", Title: "AI & automation"},
	{ID: "advanced", Title: "Advanced"},
}

// commandGroupByName maps each top-level command to its group. Hidden aliases
// (login, whoami) are mapped too so every child has a group and no empty
// "Additional Commands" header renders.
var commandGroupByName = map[string]string{
	"auth": "start", "login": "start", "whoami": "start", "projects": "start",
	"profiles": "start", "browse": "start", "open": "start", "setup": "start",
	"paywalls": "design", "fonts": "design", "media-assets": "design",
	"offerings": "catalog", "products": "catalog", "packages": "catalog", "entitlements": "catalog",
	"customer": "revenue", "subscriptions": "revenue", "purchases": "revenue",
	"invoices": "revenue", "charts": "revenue", "metrics": "revenue",
	"apps": "integrations", "capital": "integrations", "webhooks": "integrations",
	"rico": "ai", "skills": "ai",
	"api": "advanced", "audit": "advanced", "schema": "advanced",
	"commands": "advanced", "version": "advanced",
}

// applyCommandGroups registers the groups on the root and assigns each command
// (and the generated help/completion commands) to one, so `rc --help` renders
// as labeled sections instead of one flat list.
func applyCommandGroups(root *cobra.Command) {
	root.AddGroup(commandGroups...)
	for _, c := range root.Commands() {
		if g, ok := commandGroupByName[c.Name()]; ok {
			c.GroupID = g
		}
	}
	root.SetHelpCommandGroupID("advanced")
	root.SetCompletionCommandGroupID("advanced")
}

// applyHelpStyling registers the help template funcs and installs the styled
// usage template on the root; cobra inherits it to every subcommand.
func applyHelpStyling(root *cobra.Command) {
	cobra.AddTemplateFunc("rcHead", func(s string) string { return output.HelpHeader(helpColor, s) })
	cobra.AddTemplateFunc("rcCmd", func(s string) string { return output.HelpCommand(helpColor, s) })
	cobra.AddTemplateFunc("rcFoot", func(s string) string { return output.HelpDim(helpColor, s) })
	cobra.AddTemplateFunc("rcGlobalFlags", globalFlagsSummary)
	root.SetUsageTemplate(rcUsageTemplate)
}

// globalFlagsSummary collapses the inherited-flag block into a compact dimmed
// line — the flag names plus a pointer to the root help, where each is
// described in full. The full block appears only at the root, where these
// flags are local rather than inherited.
func globalFlagsSummary(c *cobra.Command) string {
	rootFlags := c.Root().PersistentFlags()
	var globals []string
	midLevel := pflag.NewFlagSet("inherited", pflag.ContinueOnError)
	c.InheritedFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		if rootFlags.Lookup(f.Name) != nil {
			globals = append(globals, "--"+f.Name)
			return
		}
		// Inherited from an intermediate parent (a group's persistent flag),
		// not a root global — the root help doesn't document it, so show it in
		// full rather than collapsing it into the pointer below.
		midLevel.AddFlag(f)
	})

	var b strings.Builder
	if usage := strings.TrimRight(midLevel.FlagUsages(), "\n"); usage != "" {
		b.WriteString(usage + "\n\n")
	}
	if len(globals) > 0 {
		b.WriteString(output.HelpHeader(helpColor, "Global flags:") + " " +
			output.HelpDim(helpColor, strings.Join(globals, ", ")) + "\n" +
			output.HelpDim(helpColor, "Run `"+c.Root().Name()+" --help` for what each global flag does."))
	}
	return b.String()
}
