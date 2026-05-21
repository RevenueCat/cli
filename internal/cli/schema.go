package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// LLM-introspection commands: `rc commands` (tree) and `rc schema <cmd>`
// (per-command flag + arg + example schema). These let an agent discover the
// full surface without scraping --help.

func newSchemaCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "schema [command...]",
		Short: "Print the JSON schema for a command's flags, args, and examples",
		Long: `Print a machine-readable description of a command. Includes:
  - command name, aliases, short and long descriptions
  - positional argument hints (parsed from the Use field)
  - flag schema (name, shorthand, type, default, description)
  - example invocations (if defined)
  - subcommand names

Always emits JSON regardless of --json (the command is purely informational).
Use this from an agent rather than scraping the human --help output.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			target, _, err := root.Find(args)
			if err != nil {
				return err
			}
			return rt.Out.Render(commandSchema(target))
		},
	}
}

func newCommandsCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "commands",
		Short: "Print the full command tree (for agent discovery)",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			return rt.Out.Render(commandTree(root))
		},
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version (works under --json too)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			return rt.Out.Render(map[string]any{
				"version": cmd.Root().Version,
			})
		},
	}
}

func commandSchema(c *cobra.Command) map[string]any {
	flags := []map[string]any{}
	addFlag := func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		flags = append(flags, map[string]any{
			"name":        f.Name,
			"shorthand":   f.Shorthand,
			"type":        f.Value.Type(),
			"default":     f.DefValue,
			"description": f.Usage,
		})
	}
	c.Flags().VisitAll(addFlag)
	c.InheritedFlags().VisitAll(addFlag)

	subs := []string{}
	for _, sc := range c.Commands() {
		if sc.Hidden {
			continue
		}
		subs = append(subs, sc.Name())
	}

	return map[string]any{
		"name":        c.Name(),
		"path":        commandPath(c),
		"aliases":     c.Aliases,
		"use":         c.Use,
		"short":       c.Short,
		"long":        c.Long,
		"args":        parseArgsFromUse(c.Use),
		"example":     c.Example,
		"flags":       flags,
		"subcommands": subs,
		"runnable":    c.Runnable(),
	}
}

// parseArgsFromUse extracts <required> and [optional] tokens from a cobra Use
// string so agents can know the positional shape. Example: "show <id>" →
// [{name:"id", required:true}]; "attach <id> <product-id> [product-id...]" →
// three entries with appropriate flags.
func parseArgsFromUse(use string) []map[string]any {
	out := []map[string]any{}
	fields := strings.Fields(use)
	for _, f := range fields {
		if len(f) < 2 {
			continue
		}
		first, last := f[0], f[len(f)-1]
		if first == '<' && last == '>' {
			name := strings.TrimSuffix(f[1:len(f)-1], "...")
			out = append(out, map[string]any{
				"name":     name,
				"required": true,
				"variadic": strings.HasSuffix(f, "...>"),
			})
		} else if first == '[' && last == ']' {
			name := strings.TrimSuffix(f[1:len(f)-1], "...")
			out = append(out, map[string]any{
				"name":     name,
				"required": false,
				"variadic": strings.HasSuffix(f, "...]"),
			})
		}
	}
	return out
}

func commandPath(c *cobra.Command) string {
	parts := []string{c.Name()}
	for p := c.Parent(); p != nil; p = p.Parent() {
		parts = append([]string{p.Name()}, parts...)
	}
	return strings.Join(parts, " ")
}

func commandTree(c *cobra.Command) map[string]any {
	subs := []map[string]any{}
	for _, sc := range c.Commands() {
		if sc.Hidden {
			continue
		}
		subs = append(subs, commandTree(sc))
	}
	return map[string]any{
		"name":     c.Name(),
		"short":    c.Short,
		"aliases":  c.Aliases,
		"commands": subs,
	}
}
