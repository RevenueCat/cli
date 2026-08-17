package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/revenuecat/cli/internal/output"
)

// helpColor gates help styling; the root help func sets it before each render.
var helpColor = true

// rcUsageTemplate styles the usage sections and collapses inherited flags via rcGlobalFlags.
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
	{ID: "start", Title: "Get started"},
	{ID: "design", Title: "Design paywalls"},
	{ID: "catalog", Title: "Manage your catalog"},
	{ID: "revenue", Title: "Manage customers & revenue"},
	{ID: "integrations", Title: "Connect apps & integrations"},
	{ID: "ai", Title: "Automate with AI"},
	{ID: "advanced", Title: "Advanced"},
}

// groupTitle returns a group's display title from commandGroups, the single
// source of truth shared by `rc --help` and the home screen.
func groupTitle(id string) string {
	for _, g := range commandGroups {
		if g.ID == id {
			return g.Title
		}
	}
	return id
}

// applyCommandGroups registers the help groups. Each command's GroupID is set
// where it's added to root (see NewRootCmd), so grouping lives on the command.
func applyCommandGroups(root *cobra.Command) {
	root.AddGroup(commandGroups...)
	root.SetHelpCommandGroupID("advanced")
	root.SetCompletionCommandGroupID("advanced")
}

// applyHelpStyling installs the styled usage template and its template funcs.
func applyHelpStyling(root *cobra.Command) {
	cobra.AddTemplateFunc("rcHead", func(s string) string { return output.HelpHeader(helpColor, s) })
	cobra.AddTemplateFunc("rcCmd", func(s string) string { return output.HelpCommand(helpColor, s) })
	cobra.AddTemplateFunc("rcFoot", func(s string) string { return output.HelpDim(helpColor, s) })
	cobra.AddTemplateFunc("rcGlobalFlags", globalFlagsSummary)
	root.SetUsageTemplate(rcUsageTemplate)
}

// globalFlagsSummary collapses root global flags to a one-line pointer; other
// inherited flags are shown in full.
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
		midLevel.AddFlag(f) // not a root global — show in full
	})

	var b strings.Builder
	if usage := strings.TrimRight(midLevel.FlagUsages(), "\n"); usage != "" {
		b.WriteString(output.HelpHeader(helpColor, "Inherited flags:") + "\n" + usage + "\n\n")
	}
	if len(globals) > 0 {
		b.WriteString(output.HelpHeader(helpColor, "Global flags:") + " " +
			output.HelpDim(helpColor, strings.Join(globals, ", ")) + "\n" +
			output.HelpDim(helpColor, "Run `"+c.Root().Name()+" --help` for what each global flag does."))
	}
	return b.String()
}
