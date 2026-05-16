package cli

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// `rc schema <command>` and `rc commands --json` are LLM-introspection commands.
// They let an agent discover the full command tree and per-command flag schema
// without scraping --help.

func newSchemaCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "schema [command...]",
		Short: "Print the JSON schema for a command's flags and output",
		Args:  cobra.MinimumNArgs(1),
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

func commandSchema(c *cobra.Command) map[string]any {
	flags := []map[string]any{}
	c.Flags().VisitAll(func(f *pflag.Flag) {
		flags = append(flags, map[string]any{
			"name":        f.Name,
			"shorthand":   f.Shorthand,
			"type":        f.Value.Type(),
			"default":     f.DefValue,
			"description": f.Usage,
		})
	})
	return map[string]any{
		"name":  c.Name(),
		"use":   c.Use,
		"short": c.Short,
		"long":  c.Long,
		"flags": flags,
	}
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
		"commands": subs,
	}
}
