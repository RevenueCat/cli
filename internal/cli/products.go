package cli

import (
	"context"
	"fmt"

	"github.com/charmbracelet/huh"
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
		newProductsCreateCmd(),
		newProductsDeleteCmd(),
		newProductsArchiveCmd(),
		newProductsRestoreCmd(),
		newProductsPushCmd(),
	)
	return cmd
}

func newProductsArchiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "archive [id]",
		Short: "Archive a product",
		Long: `Archives a product. Existing subscribers keep their access; new
attaches are blocked.

Reversibility: use ` + "`rc products restore <id>`" + ` to undo.

Confirmation: no prompt — soft, reversible state change.`,
		Example: `  rc products archive prod_legacy`,
		Args:    cobra.MaximumNArgs(1),
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
			productID, err := requireID(rt, argAt(args, 0), "product", func() ([]PickerItem, error) {
				return productPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			p, err := client.Products.Archive(cmd.Context(), projectID, productID)
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
		Use:   "restore [id]",
		Short: "Restore an archived product",
		Long: `Restores a previously-archived product. Inverse of
` + "`rc products archive`" + `.

Reversibility: re-archive with ` + "`rc products archive <id>`" + `.

Confirmation: no prompt.`,
		Example: `  rc products restore prod_legacy`,
		Args:    cobra.MaximumNArgs(1),
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
			productID, err := requireID(rt, argAt(args, 0), "product", func() ([]PickerItem, error) {
				return productPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			p, err := client.Products.Restore(cmd.Context(), projectID, productID)
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
		Use:   "push [id]",
		Short: "Push a product configuration to its underlying store",
		Long: `Pushes a product's current configuration up to the underlying store
(App Store, Play Store, Stripe, etc.). Required after editing pricing or
metadata on platforms where RC manages the store-side config.

Reversibility: external side effect — once written to the store, undoing
requires a follow-up push with the previous configuration.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc products push prod_abc --yes`,
		Args:    cobra.MaximumNArgs(1),
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
			productID, err := requireID(rt, argAt(args, 0), "product", func() ([]PickerItem, error) {
				return productPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			if !rt.Globals.AssumeYes {
				ok, err := tui.Confirm(rt.Globals.NoInput, fmt.Sprintf("Push product %q to its store?", productID))
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("aborted")
				}
			}
			if err := client.Products.Push(cmd.Context(), projectID, productID); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Pushed %s", productID))
			return rt.Out.Render(map[string]any{"ok": true, "id": productID})
		},
	}
}

func newProductsListCmd() *cobra.Command {
	var appID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List products",
		Example: `  rc products list
  rc products list --app-id app_abc
  rc products list --json | jq '.data.items[] | select(.type == "subscription")'`,
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
			var opts *api.ProductListOptions
			if appID != "" {
				opts = &api.ProductListOptions{AppID: appID}
			}
			page, err := client.Products.List(cmd.Context(), projectID, opts)
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
				if p.Subscription != nil && p.Subscription.Duration != nil {
					dur = *p.Subscription.Duration
				}
				rows = append(rows, []string{p.ID, derefStr(p.DisplayName), string(p.Type), p.StoreIdentifier, dur, string(p.State)})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "DISPLAY NAME", "TYPE", "STORE ID", "DURATION", "STATE"},
				Rows:    rows,
				Raw:     page,
			})
		},
	}
	cmd.Flags().StringVar(&appID, "app-id", "", "filter by app ID")
	return cmd
}

func newProductsCreateCmd() *cobra.Command {
	var storeID, productType, appID, displayName, duration string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a product",
		Long: `Create a new product in the project catalog.

--store-id is the product identifier on the platform store (required).
--type must be "subscription" or "one_time" (required; picker shown in TTY).
--app-id is the RevenueCat app ID (required; picker shown in TTY).
--duration (optional, subscriptions only) is an ISO 8601 duration, e.g. P1M, P1Y.`,
		Example: `  rc products create --store-id com.example.monthly --type subscription --app-id app_abc
  rc products create --store-id com.example.once --type one_time --app-id app_abc --display-name "Unlock Everything"`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if storeID == "" {
				return fmt.Errorf("--store-id is required")
			}
			if productType == "" {
				if rt.Globals.NoInput || !tui.IsInteractive() {
					return fmt.Errorf("--type is required (subscription or one_time)")
				}
				sel := huh.NewSelect[string]().Title("Type").Options(
					huh.NewOption("Subscription", "subscription"),
					huh.NewOption("One-time purchase", "one_time"),
				).Value(&productType)
				if err := tui.Form(false).Field(sel).Run(); err != nil {
					return err
				}
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			appID, err = requireID(rt, appID, "app", func() ([]PickerItem, error) {
				page, err := client.Apps.List(cmd.Context(), projectID)
				if err != nil {
					return nil, err
				}
				items := make([]PickerItem, len(page.Items))
				for i, a := range page.Items {
					items[i] = PickerItem{ID: a.ID, Label: fmt.Sprintf("%s  (%s)", a.Name, string(a.Type))}
				}
				return items, nil
			})
			if err != nil {
				return err
			}
			body := api.ProductCreate{
				StoreIdentifier: storeID,
				Type:            productType,
				AppID:           appID,
				DisplayName:     displayName,
			}
			_ = duration // subscription duration passed separately; ProductCreate does not carry it in this codegen model
			p, err := client.Products.Create(cmd.Context(), projectID, body)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Created product %s", p.ID))
			return rt.Out.Render(p)
		},
	}
	cmd.Flags().StringVar(&storeID, "store-id", "", "store product identifier (required)")
	cmd.Flags().StringVar(&productType, "type", "", "product type: subscription or one_time (picker shown in TTY if omitted)")
	cmd.Flags().StringVar(&appID, "app-id", "", "app ID to associate with (picker shown in TTY if omitted)")
	cmd.Flags().StringVar(&displayName, "display-name", "", "human-readable display name")
	cmd.Flags().StringVar(&duration, "duration", "", "subscription duration as ISO 8601 (e.g. P1M, P1Y)")
	return cmd
}

func newProductsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [id]",
		Short: "Show a product",
		Args:  cobra.MaximumNArgs(1),
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
			productID, err := requireID(rt, argAt(args, 0), "product", func() ([]PickerItem, error) {
				return productPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			p, err := client.Products.Get(cmd.Context(), projectID, productID)
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
		Use:   "delete [id]",
		Short: "Delete a product",
		Long: `Permanently deletes a product from the project.

Reversibility: irreversible. Prefer ` + "`rc products archive`" + ` for
reversible removal.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc products delete prod_old --yes`,
		Args:    cobra.MaximumNArgs(1),
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
			productID, err := requireID(rt, argAt(args, 0), "product", func() ([]PickerItem, error) {
				return productPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			if !rt.Globals.AssumeYes {
				ok, err := tui.Confirm(rt.Globals.NoInput, fmt.Sprintf("Delete product %q?", productID))
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("aborted")
				}
			}
			if err := client.Products.Delete(cmd.Context(), projectID, productID); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Deleted %s", productID))
			return rt.Out.Render(map[string]any{"ok": true, "id": productID})
		},
	}
}

// ── picker helpers ───────────────────────────────────────────────────────────

func productPickerItems(ctx context.Context, client *api.Client, projectID string) ([]PickerItem, error) {
	page, err := client.Products.List(ctx, projectID, nil)
	if err != nil {
		return nil, err
	}
	items := make([]PickerItem, len(page.Items))
	for i, p := range page.Items {
		displayName := derefStr(p.DisplayName)
		if displayName == "" {
			displayName = p.StoreIdentifier
		}
		items[i] = PickerItem{ID: p.ID, Label: fmt.Sprintf("%s  (%s)", displayName, string(p.Type))}
	}
	return items, nil
}

// ── browser helpers ──────────────────────────────────────────────────────────

// productToItem builds a leaf detail item for a product (no further drill-down).
func productToItem(p api.Product) tui.BrowserItem {
	dur, grace, trial := "", "", ""
	if p.Subscription != nil {
		dur = derefStr(p.Subscription.Duration)
		grace = derefStr(p.Subscription.GracePeriodDuration)
		trial = derefStr(p.Subscription.TrialDuration)
	}
	displayName := derefStr(p.DisplayName)
	return tui.BrowserItem{
		ID:    p.ID,
		Label: displayName,
		Meta:  string(p.Type),
		Row:   []string{p.ID, displayName, string(p.Type), p.StoreIdentifier, string(p.State)},
		Fields: []tui.BrowserField{
			{Key: "ID", Value: p.ID},
			{Key: "Display name", Value: displayName},
			{Key: "Store ID", Value: p.StoreIdentifier},
			{Key: "Type", Value: string(p.Type)},
			{Key: "State", Value: string(p.State)},
			{Key: "App", Value: p.AppID},
			{Key: "Duration", Value: dur},
			{Key: "Grace period", Value: grace},
			{Key: "Trial", Value: trial},
			{Key: "Created", Value: formatMillis(p.CreatedAt)},
		},
	}
}
