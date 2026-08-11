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
		Example: `  rc schema apps create
  rc schema rico`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			target, _, err := root.Find(args)
			if err != nil {
				return err
			}
			return rt.Out.RenderJSON(commandSchema(target))
		},
	}
}

func newCommandsCmd(root *cobra.Command) *cobra.Command {
	var schemas bool
	cmd := &cobra.Command{
		Use:   "commands",
		Short: "Print the full command tree (for agent discovery)",
		Long: `Prints the command tree as JSON. Pass --schemas to include every command's
full schema (flags, args, examples) in one call — cheaper for an agent than
running rc schema per command.`,
		Example: `  rc commands
  rc commands --schemas`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			if schemas {
				return rt.Out.RenderJSON(commandTreeWithSchemas(root))
			}
			return rt.Out.RenderJSON(commandTree(root))
		},
	}
	cmd.Flags().BoolVar(&schemas, "schemas", false, "include full flag/arg/example schemas for every command")
	return cmd
}

// commandTreeWithSchemas is the whole surface in one document: the agent
// answer to "stop researching the CLI one command at a time".
func commandTreeWithSchemas(c *cobra.Command) map[string]any {
	tree := commandSchema(c)
	subs := []map[string]any{}
	for _, sc := range c.Commands() {
		if experimentalFromSchema(sc) {
			continue
		}
		subs = append(subs, commandTreeWithSchemas(sc))
	}
	tree["commands"] = subs
	delete(tree, "subcommands")
	return tree
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

	subs := []map[string]any{}
	for _, sc := range c.Commands() {
		if experimentalFromSchema(sc) {
			continue
		}
		subs = append(subs, map[string]any{
			"name":     sc.Name(),
			"short":    sc.Short,
			"aliases":  sc.Aliases,
			"runnable": sc.Runnable(),
		})
	}

	schema := map[string]any{
		"name":         c.Name(),
		"path":         commandPath(c),
		"group":        c.GroupID,
		"aliases":      c.Aliases,
		"use":          c.Use,
		"short":        c.Short,
		"long":         c.Long,
		"args":         parseArgsFromUse(c.Use),
		"example":      c.Example,
		"flags":        flags,
		"subcommands":  subs,
		"runnable":     c.Runnable(),
		"capabilities": inferCapabilities(c),
	}
	addHumanRequirement(schema, c)
	return schema
}

// addHumanRequirement surfaces the requires_human annotation so agents
// reading `rc commands --json` / `rc schema` know a command must be handed
// to the user (e.g. Apple sign-in with 2FA) rather than run directly.
func addHumanRequirement(schema map[string]any, c *cobra.Command) {
	if c.Annotations["requires_human"] != "true" {
		return
	}
	schema["requires_human"] = true
	if reason := c.Annotations["requires_human_reason"]; reason != "" {
		schema["requires_human_reason"] = reason
	}
}

// inferCapabilities lists runnable descendants as `group:verb` labels relative to c.
func inferCapabilities(c *cobra.Command) []string {
	acc := &capAccumulator{seen: map[string]bool{}}
	if c.Runnable() {
		acc.add(canonicalVerb(c.Name()))
	}
	for _, sc := range c.Commands() {
		if experimentalFromSchema(sc) {
			continue
		}
		collectCapabilities(sc, []string{sc.Name()}, acc)
	}
	return acc.caps
}

func collectCapabilities(c *cobra.Command, path []string, acc *capAccumulator) {
	if c.Runnable() {
		acc.add(capLabel(path))
	}
	for _, sc := range c.Commands() {
		if experimentalFromSchema(sc) {
			continue
		}
		collectCapabilities(sc, append(append([]string{}, path...), sc.Name()), acc)
	}
}

func capLabel(path []string) string {
	if len(path) == 0 {
		return ""
	}
	out := append([]string{}, path[:len(path)-1]...)
	out = append(out, canonicalVerb(path[len(path)-1]))
	return strings.Join(out, ":")
}

func canonicalVerb(name string) string {
	switch name {
	case "get":
		return "show"
	default:
		return name
	}
}

type capAccumulator struct {
	seen map[string]bool
	caps []string
}

func (a *capAccumulator) add(label string) {
	if label == "" || a.seen[label] {
		return
	}
	a.seen[label] = true
	a.caps = append(a.caps, label)
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
		if experimentalFromSchema(sc) {
			continue
		}
		subs = append(subs, commandTree(sc))
	}
	tree := map[string]any{
		"name":         c.Name(),
		"path":         commandPath(c),
		"group":        c.GroupID,
		"short":        c.Short,
		"aliases":      c.Aliases,
		"runnable":     c.Runnable(),
		"capabilities": inferCapabilities(c),
		"commands":     subs,
	}
	addHumanRequirement(tree, c)
	return tree
}
