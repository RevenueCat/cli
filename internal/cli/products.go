package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

// Products: v2 API supports list/show/delete but not create/update via API
// (products are typically created by the store sync). Surface what's real.

func newProductsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "products",
		Aliases: []string{"product"},
		Short:   "Inspect products in the project catalog",
	}
	cmd.AddCommand(
		newProductsListCmd(),
		newProductsShowCmd(),
		newProductsDeleteCmd(),
		newProductsArchiveCmd(),
		newProductsRestoreCmd(),
		newProductsPushCmd(),
	)
	return cmd
}

func newProductsArchiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "archive <id>",
		Short: "Archive a product",
		Long: `Archives a product. Existing subscribers keep their access; new
attaches are blocked.

Reversibility: use ` + "`rc products restore <id>`" + ` to undo.

Confirmation: no prompt — soft, reversible state change.`,
		Example: `  rc products archive prod_legacy`,
		Args:    cobra.ExactArgs(1),
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
			p, err := client.Products.Archive(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Archived %s", p.ID))
			return rt.Out.Render(p)
		},
	}
}

func newProductsRestoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore <id>",
		Short: "Restore an archived product",
		Long: `Restores a previously-archived product. Inverse of
` + "`rc products archive`" + `.

Reversibility: re-archive with ` + "`rc products archive <id>`" + `.

Confirmation: no prompt.`,
		Example: `  rc products restore prod_legacy`,
		Args:    cobra.ExactArgs(1),
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
			p, err := client.Products.Restore(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Restored %s", p.ID))
			return rt.Out.Render(p)
		},
	}
}

func newProductsPushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push <id>",
		Short: "Push a product configuration to its underlying store",
		Long: `Pushes a product's current configuration up to the underlying store
(App Store, Play Store, Stripe, etc.). Required after editing pricing or
metadata on platforms where RC manages the store-side config.

Reversibility: external side effect — once written to the store, undoing
requires a follow-up push with the previous configuration.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc products push prod_abc --yes`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if !rt.Globals.AssumeYes {
				ok, err := tui.Confirm(rt.Globals.NoInput, fmt.Sprintf("Push product %q to its store?", args[0]))
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
			if err := client.Products.Push(cmd.Context(), projectID, args[0]); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Pushed %s", args[0]))
			return rt.Out.Render(map[string]any{"ok": true, "id": args[0]})
		},
	}
}

func newProductsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List products",
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
			page, err := client.Products.List(cmd.Context(), projectID)
			if err != nil {
				return err
			}

			if !rt.Globals.JSON && !rt.Globals.NoInput && tui.IsInteractive() {
				items := make([]tui.BrowserItem, len(page.Items))
				for i, p := range page.Items {
					items[i] = productToItem(p)
				}
				return tui.RunBrowserTable("Products", []string{"ID", "DISPLAY NAME", "TYPE", "STORE ID", "STATE"}, items)
			}

			rows := make([][]string, 0, len(page.Items))
			for _, p := range page.Items {
				dur := ""
				if p.Subscription != nil {
					dur = p.Subscription.Duration
				}
				rows = append(rows, []string{p.ID, p.DisplayName, p.Type, p.StoreIdentifier, dur, p.State})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "DISPLAY NAME", "TYPE", "STORE ID", "DURATION", "STATE"},
				Rows:    rows,
				Raw:     page,
			})
		},
	}
}

func newProductsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show a product",
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
			p, err := client.Products.Get(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			if !rt.Globals.JSON && !rt.Globals.NoInput && tui.IsInteractive() {
				item := productToItem(*p)
				return tui.RunBrowser("Product", []tui.BrowserItem{item})
			}
			return rt.Out.Render(p)
		},
	}
}

func newProductsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a product",
		Long: `Permanently deletes a product from the project.

Reversibility: irreversible. Prefer ` + "`rc products archive`" + ` for
reversible removal.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc products delete prod_old --yes`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if !rt.Globals.AssumeYes {
				ok, err := tui.Confirm(rt.Globals.NoInput, fmt.Sprintf("Delete product %q?", args[0]))
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
			if err := client.Products.Delete(cmd.Context(), projectID, args[0]); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Deleted %s", args[0]))
			return rt.Out.Render(map[string]any{"ok": true, "id": args[0]})
		},
	}
}

// ── browser helpers ──────────────────────────────────────────────────────────

// productToItem builds a leaf detail item for a product (no further drill-down).
func productToItem(p api.Product) tui.BrowserItem {
	dur, grace, trial := "", "", ""
	if p.Subscription != nil {
		dur = p.Subscription.Duration
		grace = p.Subscription.GracePeriodDuration
		trial = p.Subscription.TrialDuration
	}
	return tui.BrowserItem{
		ID:    p.ID,
		Label: p.DisplayName,
		Meta:  p.Type,
		Row:   []string{p.ID, p.DisplayName, p.Type, p.StoreIdentifier, p.State},
		Fields: []tui.BrowserField{
			{Key: "ID", Value: p.ID},
			{Key: "Display name", Value: p.DisplayName},
			{Key: "Store ID", Value: p.StoreIdentifier},
			{Key: "Type", Value: p.Type},
			{Key: "State", Value: p.State},
			{Key: "App", Value: p.AppID},
			{Key: "Duration", Value: dur},
			{Key: "Grace period", Value: grace},
			{Key: "Trial", Value: trial},
			{Key: "Created", Value: formatMillis(p.CreatedAt)},
		},
	}
}
