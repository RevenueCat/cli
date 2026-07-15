package cli

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

const (
	officialToolkitSource = "RevenueCat/ai-toolkit"
	officialToolkitDocs   = "https://www.revenuecat.com/docs/tools/overview"
)

type skillsInstaller interface {
	Run(*cobra.Command, []string) error
}

type npxSkillsInstaller struct{}

func (npxSkillsInstaller) Run(cmd *cobra.Command, args []string) error {
	path, err := exec.LookPath("npx")
	if err != nil {
		return fmt.Errorf("npx is required to install the RevenueCat AI Toolkit; install Node.js or follow %s: %w", officialToolkitDocs, err)
	}
	child := exec.CommandContext(cmd.Context(), path, args...)
	child.Stdout = cmd.ErrOrStderr()
	child.Stderr = cmd.ErrOrStderr()
	child.Stdin = cmd.InOrStdin()
	return child.Run()
}

func newSkillsCmd() *cobra.Command {
	return newSkillsCmdWithInstaller(npxSkillsInstaller{})
}

func newSkillsCmdWithInstaller(installer skillsInstaller) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "skills",
		Aliases: []string{"skill"},
		Short:   "Install the official RevenueCat AI Toolkit",
		Long: `RevenueCat's official AI Toolkit provides maintained agent workflows for
project setup, SDK integration, catalog management, and project health checks.

rc delegates to the standard Skills CLI instead of embedding a stale copy of
those workflows. Marketplace installation options for Codex, Claude Code,
Cursor, VS Code, and Gemini are documented on the RevenueCat website.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			return rt.Out.Render(map[string]any{
				"source":          officialToolkitSource,
				"install_command": "npx skills add " + officialToolkitSource,
				"docs_url":        officialToolkitDocs,
			})
		},
	}
	cmd.AddCommand(newSkillsInstallCmd(installer))
	return cmd
}

func newSkillsInstallCmd(installer skillsInstaller) *cobra.Command {
	var global, copyFiles bool
	var agents, skills []string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Pull and install skills from RevenueCat/ai-toolkit",
		Long: `Pulls the current official RevenueCat skills by delegating to:

  npx skills add RevenueCat/ai-toolkit

The standard Skills CLI detects supported agents and owns installation paths,
lock files, security review, and updates. This command does not vendor or cache
a separate copy of the toolkit inside rc. Under --no-input, pass --yes.`,
		Example: `  rc skills install
  rc skills install --global
  rc skills install --agent codex --yes --no-input
  rc skills install --skill create-revenuecat-project --yes --no-input`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			if rt.Globals.NoInput && !rt.Globals.AssumeYes {
				return errors.New("installing skills under --no-input requires --yes")
			}
			args := []string{"--yes", "skills", "add", officialToolkitSource}
			if global {
				args = append(args, "--global")
			}
			if len(agents) > 0 {
				args = append(args, "--agent")
				args = append(args, agents...)
			}
			if len(skills) > 0 {
				args = append(args, "--skill")
				args = append(args, skills...)
			}
			if copyFiles {
				args = append(args, "--copy")
			}
			if rt.Globals.AssumeYes {
				args = append(args, "--yes")
			}
			if err := installer.Run(cmd, args); err != nil {
				return fmt.Errorf("install RevenueCat AI Toolkit: %w", err)
			}
			scope := "project"
			if global {
				scope = "global"
			}
			rt.Out.Success("Installed the RevenueCat AI Toolkit")
			return rt.Out.Render(map[string]any{
				"installed": true,
				"source":    officialToolkitSource,
				"scope":     scope,
				"agents":    agents,
				"skills":    skills,
				"command":   "npx " + strings.Join(args[1:], " "),
				"docs_url":  officialToolkitDocs,
			})
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "install globally instead of in the current project")
	cmd.Flags().StringSliceVar(&agents, "agent", nil, "agents to install for (passed to the standard Skills CLI)")
	cmd.Flags().StringSliceVar(&skills, "skill", nil, "specific toolkit skills to install")
	cmd.Flags().BoolVar(&copyFiles, "copy", false, "copy skill files instead of symlinking them")
	return cmd
}
