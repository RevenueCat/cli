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

{{rcHead "Available Commands:"}}{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rcCmd (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{rcHead "Flags:"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

{{rcGlobalFlags .}}{{end}}{{if .HasHelpSubCommands}}

{{rcHead "Additional help topics:"}}{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rcCmd (rpad .CommandPath .CommandPathPadding)}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

{{rcFoot (printf "Use \"%s [command] --help\" for more information about a command." .CommandPath)}}{{end}}
`

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
	var names []string
	c.InheritedFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		names = append(names, "--"+f.Name)
	})
	return output.HelpHeader(helpColor, "Global flags:") + " " +
		output.HelpDim(helpColor, strings.Join(names, ", ")) + "\n" +
		output.HelpDim(helpColor, "Run `"+c.Root().Name()+" --help` for what each global flag does.")
}
