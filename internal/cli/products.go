package cli

import (
	"context"
	"fmt"
	"strings"

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
		newProductsUpdateCmd(),
		newProductsDeleteCmd(),
		newProductsArchiveCmd(),
		newProductsRestoreCmd(),
		newProductsPushCmd(),
		newProductsPricesCmd(),
		newProductsStoreCmd(),
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
			if err := confirmOrAbort(rt, fmt.Sprintf("Push product %q to its store?", productID)); err != nil {
				return err
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
	var storeID, productType, appID, displayName, title, duration string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a product",
		Long: `Create a new product in the project catalog.

--store-id is the product identifier on the platform store (required).
--type must be "subscription" or "one_time" (required; picker shown in TTY).
--app-id is the RevenueCat app ID (required; picker shown in TTY).
--title is the user-facing product title required by Test Store products.
--duration (optional, subscriptions only) is an ISO 8601 duration, e.g. P1M, P1Y.`,
		Example: `  rc products create --store-id premium_monthly --type subscription --app-id app_test --title "Premium Monthly" --duration P1M
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
			if productType != "subscription" && productType != "one_time" {
				return fmt.Errorf("--type must be 'subscription' or 'one_time', got %q", productType)
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
				Title:           title,
			}
			if productType == "subscription" && duration != "" {
				body.Subscription = &api.ProductSubscriptionInput{Duration: api.Duration(duration)}
			}
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
	cmd.Flags().StringVar(&title, "title", "", "user-facing product title (required for Test Store products)")
	cmd.Flags().StringVar(&duration, "duration", "", "subscription duration as ISO 8601 (e.g. P1M, P1Y)")
	return cmd
}

func newProductsPricesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prices [product-id]",
		Short: "List Test Store or Web Billing product prices",
		Long: `Lists configured prices for a Test Store or Web Billing product.

Use prices set to idempotently create missing currencies or update existing
Test Store prices. Amounts are entered as decimal major units, not micros.`,
		Example: `  rc products prices prod_abc --json --no-input
  rc products prices set prod_abc --price USD=9.99 --price EUR=8.99 --json --no-input`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, productID, client, err := productPriceContext(cmd, args)
			if err != nil {
				return err
			}
			prices, err := client.Products.ListPrices(cmd.Context(), projectID, productID)
			if err != nil {
				return err
			}
			return renderProductPrices(rt, prices)
		},
	}
	cmd.AddCommand(newProductsPricesSetCmd())
	return cmd
}

func newProductsPricesSetCmd() *cobra.Command {
	var rawPrices []string
	cmd := &cobra.Command{
		Use:   "set [product-id]",
		Short: "Create or update Test Store product prices",
		Long: `Sets exact Test Store product prices by currency. Missing currencies are
created through the Test Store price API; existing currencies are updated.
Running the same command again is safe and makes no unnecessary writes.

Values use ISO 4217 currency and decimal major units: --price USD=9.99.`,
		Example: `  rc products prices set prod_abc --price USD=9.99
  rc products prices set prod_abc --price USD=9.99 --price EUR=8.99 --json --no-input`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			desired, err := parseProductPrices(rawPrices)
			if err != nil {
				return err
			}
			projectID, productID, client, err := productPriceContext(cmd, args)
			if err != nil {
				return err
			}
			existing, err := client.Products.ListPrices(cmd.Context(), projectID, productID)
			if err != nil {
				return err
			}
			byCurrency := make(map[string]api.ProductPrice, len(existing))
			for _, price := range existing {
				byCurrency[strings.ToUpper(price.Currency)] = price
			}
			missing := make([]api.ProductPriceInput, 0)
			for _, price := range desired {
				current, ok := byCurrency[price.Currency]
				if !ok {
					missing = append(missing, price)
					continue
				}
				if current.AmountMicros == price.AmountMicros {
					continue
				}
				if _, err := client.Products.UpdatePrice(cmd.Context(), projectID, productID, price.Currency, api.ProductPriceUpdate{AmountMicros: price.AmountMicros}); err != nil {
					return err
				}
			}
			if len(missing) > 0 {
				if _, err := client.Products.CreateTestStorePrices(cmd.Context(), projectID, productID, api.ProductPricesCreate{Prices: missing}); err != nil {
					return err
				}
			}
			prices, err := client.Products.ListPrices(cmd.Context(), projectID, productID)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Configured %d product price(s)", len(desired)))
			return renderProductPrices(rt, prices)
		},
	}
	cmd.Flags().StringSliceVar(&rawPrices, "price", nil, "price as CURRENCY=AMOUNT, repeatable (required)")
	_ = cmd.MarkFlagRequired("price")
	return cmd
}

func productPriceContext(cmd *cobra.Command, args []string) (string, string, *api.Client, error) {
	rt := RuntimeFrom(cmd.Context())
	projectID, err := requireProject(rt)
	if err != nil {
		return "", "", nil, err
	}
	client, err := rt.API()
	if err != nil {
		return "", "", nil, err
	}
	productID, err := requireID(rt, argAt(args, 0), "product", func() ([]PickerItem, error) {
		return productPickerItems(cmd.Context(), client, projectID)
	})
	if err != nil {
		return "", "", nil, err
	}
	return projectID, productID, client, nil
}

func parseProductPrices(values []string) ([]api.ProductPriceInput, error) {
	prices := make([]api.ProductPriceInput, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		currency, amount, ok := strings.Cut(value, "=")
		currency = strings.ToUpper(strings.TrimSpace(currency))
		amount = strings.TrimSpace(amount)
		if !ok || len(currency) != 3 || amount == "" {
			return nil, fmt.Errorf("--price %q must use CURRENCY=AMOUNT, for example USD=9.99", value)
		}
		if _, ok := seen[currency]; ok {
			return nil, fmt.Errorf("--price contains duplicate currency %s", currency)
		}
		amountMicros, err := decimalToMicros(amount)
		if err != nil {
			return nil, fmt.Errorf("--price %s: %w", currency, err)
		}
		seen[currency] = struct{}{}
		prices = append(prices, api.ProductPriceInput{Currency: currency, AmountMicros: amountMicros})
	}
	return prices, nil
}

func renderProductPrices(rt *Runtime, prices []api.ProductPrice) error {
	rows := make([][]string, len(prices))
	for i, price := range prices {
		rows[i] = []string{price.Currency, formatPriceMicros(price.AmountMicros)}
	}
	return rt.Out.RenderTable(output.Table{
		Columns: []string{"CURRENCY", "AMOUNT"},
		Rows:    rows,
		Raw:     prices,
	})
}

func formatPriceMicros(amount int64) string {
	formatted := fmt.Sprintf("%.6f", float64(amount)/1_000_000)
	formatted = strings.TrimRight(formatted, "0")
	return strings.TrimRight(formatted, ".")
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

func newProductsUpdateCmd() *cobra.Command {
	var displayName string
	cmd := &cobra.Command{
		Use:   "update [id]",
		Short: "Update a product",
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
			body := api.ProductUpdate{}
			if cmd.Flags().Changed("display-name") {
				body.DisplayName = &displayName
			}
			p, err := client.Products.Update(cmd.Context(), projectID, productID, body)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Updated %s", p.ID))
			return rt.Out.Render(p)
		},
	}
	cmd.Flags().StringVar(&displayName, "display-name", "", "new display name")
	return cmd
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
			if err := confirmOrAbort(rt, fmt.Sprintf("Delete product %q?", productID)); err != nil {
				return err
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
