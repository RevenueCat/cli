package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/tui"
)

// openSections maps rc open targets to dashboard paths under the project.
// An entry with {id} accepts an optional second argument.
// Paths verified against the dashboard router (revenuecat-app
// project-view-content.tsx); the project root is the overview.
var openSections = map[string]string{
	"overview":     "",
	"apps":         "/apps/{id}",
	"customers":    "/customers/{id}",
	"catalog":      "/product-catalog",
	"products":     "/product-catalog/product-groups/{id}",
	"paywalls":     "/paywalls/{id}",
	"experiments":  "/experiments/{id}",
	"charts":       "/charts",
	"integrations": "/integrations",
	"api-keys":     "/api-keys",
	"audit-logs":   "/audit-logs",
	"settings":     "/settings",
	"rico":         "/rico",
}

func newOpenCmd() *cobra.Command {
	var print bool
	names := make([]string, 0, len(openSections))
	for name := range openSections {
		names = append(names, name)
	}
	sort.Strings(names)

	cmd := &cobra.Command{
		Use:   "open [section] [id]",
		Short: "Open the dashboard for the active project in your browser",
		Long: `Opens app.revenuecat.com at the active project, optionally deep-linked to a
section (and a specific resource when an ID is given). Uses your existing
browser session; it does not log the browser in.

Sections: ` + strings.Join(names, ", ") + `

Prints the URL as well, so it can be copied or shared.`,
		Example: `  rc open
  rc open paywalls
  rc open customers cus_abc
  rc open apps appa76509af23 --print`,
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			section := "overview"
			if argAt(args, 0) != "" {
				section = argAt(args, 0)
			}
			path, ok := openSections[section]
			if !ok {
				return fmt.Errorf("unknown section %q; sections: %s", section, strings.Join(names, ", "))
			}
			id := argAt(args, 1)
			switch {
			case strings.Contains(path, "{id}") && id != "":
				path = strings.ReplaceAll(path, "{id}", id)
			case strings.Contains(path, "{id}"):
				path = strings.TrimSuffix(strings.ReplaceAll(path, "{id}", ""), "/")
			case id != "":
				return fmt.Errorf("section %q does not take an ID", section)
			}

			url := envOrDefault("RC_DASHBOARD_URL", "https://app.revenuecat.com") +
				"/projects/" + dashboardProjectID(projectID) + path
			if rt.Out.IsJSON() {
				return rt.Out.Render(map[string]any{"url": url})
			}
			fmt.Fprintln(cmd.OutOrStdout(), url)
			if print || rt.Globals.NoInput || !tui.IsInteractive() {
				return nil
			}
			if err := tui.OpenURL(url); err != nil {
				rt.Out.Warn("Could not open a browser: " + err.Error())
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&print, "print", false, "print the URL without opening a browser")
	return cmd
}
