package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

func newPaywallsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "paywalls",
		Aliases: []string{"paywall"},
		Short:   "Inspect paywalls (no create/update in v2 API)",
	}
	cmd.AddCommand(
		newPaywallsListCmd(),
		newPaywallsShowCmd(),
		newPaywallsDeleteCmd(),
	)
	return cmd
}

func newPaywallsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List paywalls",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			page, err := client.Paywalls.List(cmd.Context(), projectID)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(page.Items))
			for _, p := range page.Items {
				published := "—"
				if p.PublishedAt != 0 {
					published = formatMillis(int64(p.PublishedAt))
				}
				rows = append(rows, []string{p.ID, p.OfferingID, formatMillis(int64(p.CreatedAt)), published})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "OFFERING", "CREATED", "PUBLISHED"},
				Rows:    rows,
				Raw:     page,
			})
		},
	}
}

func newPaywallsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show a paywall",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			p, err := client.Paywalls.Get(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			return rt.Out.Render(p)
		},
	}
}

func newPaywallsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a paywall",
		Long: `Permanently deletes a paywall.

Reversibility: irreversible. The v2 API does not currently expose paywall
update or restore.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc paywalls delete pw_old --yes`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if !rt.Globals.AssumeYes {
				ok, err := tui.Confirm(rt.Globals.NoInput, fmt.Sprintf("Delete paywall %q?", args[0]))
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("aborted")
				}
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			if err := client.Paywalls.Delete(cmd.Context(), projectID, args[0]); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Deleted %s", args[0]))
			return rt.Out.Render(map[string]any{"ok": true, "id": args[0]})
		},
	}
}
