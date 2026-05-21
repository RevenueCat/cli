package cli

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/output"
)

//go:embed skillfiles/*.md
var skillFS embed.FS

type skill struct {
	Name        string
	Description string
	Content     string
}

func loadSkills() ([]skill, error) {
	entries, err := fs.ReadDir(skillFS, "skillfiles")
	if err != nil {
		return nil, err
	}
	var skills []skill
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := skillFS.ReadFile("skillfiles/" + e.Name())
		if err != nil {
			return nil, err
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		content := string(data)
		desc := extractFirstLine(content)
		skills = append(skills, skill{Name: name, Description: desc, Content: content})
	}
	return skills, nil
}

// extractFirstLine returns the first non-empty line of a markdown file,
// stripping any leading # heading markers.
func extractFirstLine(s string) string {
	for _, line := range strings.SplitN(s, "\n", 10) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimLeft(line, "# ")
		return line
	}
	return ""
}

func newSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "skills",
		Aliases: []string{"skill"},
		Short:   "Browse and install reusable rc workflow skills",
		Long: `Skills are step-by-step workflow guides for common RevenueCat tasks.
Each skill can be read directly or installed as a Claude Code slash command
in the current repo (or globally) so any agent working in that project can
invoke it with /project:rc-<name>.`,
		RunE: runSkillsList,
	}
	cmd.AddCommand(
		newSkillsListCmd(),
		newSkillsShowCmd(),
		newSkillsInstallCmd(),
	)
	return cmd
}

func newSkillsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available skills",
		RunE:  runSkillsList,
	}
}

func runSkillsList(cmd *cobra.Command, _ []string) error {
	rt := RuntimeFrom(cmd.Context())
	skills, err := loadSkills()
	if err != nil {
		return err
	}
	rows := make([][]string, len(skills))
	raw := make([]map[string]any, len(skills))
	for i, s := range skills {
		rows[i] = []string{s.Name, s.Description}
		raw[i] = map[string]any{"name": s.Name, "description": s.Description}
	}
	return rt.Out.RenderTable(output.Table{
		Columns: []string{"NAME", "DESCRIPTION"},
		Rows:    rows,
		Raw:     map[string]any{"items": raw},
	})
}

func newSkillsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Print a skill's content",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skills, err := loadSkills()
			if err != nil {
				return err
			}
			name := args[0]
			for _, s := range skills {
				if s.Name == name {
					_, err := fmt.Fprint(cmd.OutOrStdout(), s.Content)
					return err
				}
			}
			names := make([]string, len(skills))
			for i, s := range skills {
				names[i] = s.Name
			}
			return fmt.Errorf("unknown skill %q — available: %s", name, strings.Join(names, ", "))
		},
	}
}

func newSkillsInstallCmd() *cobra.Command {
	var global bool
	var dir string
	cmd := &cobra.Command{
		Use:   "install [name...]",
		Short: "Install skills as Claude Code slash commands",
		Long: `Writes skills as markdown files into a Claude Code commands directory.
Once installed, any agent or human using Claude Code in that project can
invoke the skill with /project:rc-<name>.

Without arguments, installs all available skills.

Install locations (in priority order):
  --dir <path>   explicit target directory
  --global       ~/.claude/commands/ (available in all projects)
  default        .claude/commands/ in the current working directory`,
		Example: `  rc skills install                          # all skills → .claude/commands/
  rc skills install setup-offering           # one skill
  rc skills install --global                 # all skills → ~/.claude/commands/
  rc skills install debug-customer --global  # one skill, globally`,
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			skills, err := loadSkills()
			if err != nil {
				return err
			}

			// Determine target directory.
			var target string
			switch {
			case dir != "":
				target = dir
			case global:
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("resolving home dir: %w", err)
				}
				target = filepath.Join(home, ".claude", "commands")
			default:
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("resolving working dir: %w", err)
				}
				target = filepath.Join(cwd, ".claude", "commands")
			}

			// Filter to requested skills if names were given.
			if len(args) > 0 {
				wanted := make(map[string]bool, len(args))
				for _, a := range args {
					wanted[a] = true
				}
				var filtered []skill
				for _, s := range skills {
					if wanted[s.Name] {
						filtered = append(filtered, s)
						delete(wanted, s.Name)
					}
				}
				for name := range wanted {
					return fmt.Errorf("unknown skill %q — run `rc skills list` to see available skills", name)
				}
				skills = filtered
			}

			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("creating %s: %w", target, err)
			}

			var installed []string
			for _, s := range skills {
				filename := "rc-" + s.Name + ".md"
				path := filepath.Join(target, filename)
				if err := os.WriteFile(path, []byte(s.Content), 0o644); err != nil {
					return fmt.Errorf("writing %s: %w", path, err)
				}
				installed = append(installed, filename)
				rt.Out.Info(fmt.Sprintf("  wrote %s", path))
			}

			rt.Out.Success(fmt.Sprintf("Installed %d skill(s) to %s", len(installed), target))
			if !global && dir == "" {
				rt.Out.Info("Commit .claude/commands/ to make these available to all contributors.")
			}
			return rt.Out.Render(map[string]any{
				"installed": installed,
				"directory": target,
			})
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "install to ~/.claude/commands/ instead of ./.claude/commands/")
	cmd.Flags().StringVar(&dir, "dir", "", "install to a specific directory")
	return cmd
}
