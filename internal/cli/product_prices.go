package cli

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
)

func newProductsPricesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prices",
		Short: "Manage product prices",
		Long: `Manage product prices.

Currently, add and update support Test Store products only. Adding a currency
that already exists returns a conflict; updating requires the currency already
has a configured price. Prices cannot be deleted once created.`,
	}
	cmd.AddCommand(
		newProductsPricesListCmd(),
		newProductsPricesAddCmd(),
		newProductsPricesUpdateCmd(),
	)
	return cmd
}

func newProductsPricesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [id]",
		Short: "List configured product prices",
		Example: `  rc products prices list prod_abc
  rc products prices list prod_abc --json`,
		Args: cobra.MaximumNArgs(1),
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
			prices, err := client.Products.ListPrices(cmd.Context(), projectID, productID)
			if err != nil {
				return err
			}
			return renderProductPrices(rt, prices)
		},
	}
}

func newProductsPricesAddCmd() *cobra.Command {
	var priceFlags []string
	var store string
	cmd := &cobra.Command{
		Use:   "add [id]",
		Short: "Add Test Store product prices",
		Long: `Adds one or more prices for a Test Store product.

Each --price value is CURRENCY:AMOUNT, for example USD:9.99. Creates prices
for currencies that do not exist yet. The API returns a conflict if a currency
already has a price; use "rc products prices list" to inspect existing prices.

Prices cannot be deleted once created — use "rc products prices update" to
change an existing price amount.

Only Test Store products are currently supported via --store test-store.`,
		Example: `  rc products prices add prod_abc --price USD:9.99
  rc products prices add prod_abc --price USD:9.99 --price EUR:8.99`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if len(priceFlags) == 0 {
				return fmt.Errorf("--price is required")
			}
			if normalizeStoreFlag(store) != "test-store" {
				return fmt.Errorf("--store %q is not supported; only test-store products have manageable prices right now", store)
			}
			prices, err := parseProductPriceFlags(priceFlags)
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
			created, err := client.Products.AddTestStorePrices(cmd.Context(), projectID, productID, prices)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Added %d price(s) to %s", len(created), productID))
			return renderProductPrices(rt, created)
		},
	}
	cmd.Flags().StringArrayVar(&priceFlags, "price", nil, "price as CURRENCY:AMOUNT, e.g. USD:9.99 (repeatable)")
	cmd.Flags().StringVar(&store, "store", "test-store", "store to add prices for; currently only test-store")
	return cmd
}

func newProductsPricesUpdateCmd() *cobra.Command {
	var priceFlag string
	cmd := &cobra.Command{
		Use:   "update [id]",
		Short: "Update an existing product price",
		Long: `Updates an existing price for one product currency.

The --price value is CURRENCY:AMOUNT, for example USD:12.99. The currency must
already exist for the product; use "rc products prices add" to create it first.

Only Test Store products are currently supported. The API returns an error for
other store types.`,
		Example: `  rc products prices update prod_abc --price USD:12.99`,
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if priceFlag == "" {
				return fmt.Errorf("--price is required")
			}
			price, err := parseProductPrice(priceFlag)
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
			updated, err := client.Products.UpdatePrice(cmd.Context(), projectID, productID, price)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Updated %s price for %s", updated.Currency, productID))
			return renderProductPrices(rt, []api.ProductPrice{*updated})
		},
	}
	cmd.Flags().StringVar(&priceFlag, "price", "", "price as CURRENCY:AMOUNT, e.g. USD:12.99")
	return cmd
}

func renderProductPrices(rt *Runtime, prices []api.ProductPrice) error {
	rows := make([][]string, 0, len(prices))
	for _, p := range prices {
		rows = append(rows, []string{p.Currency, formatMicros(p.AmountMicros)})
	}
	return rt.Out.RenderTable(output.Table{
		Columns: []string{"CURRENCY", "AMOUNT"},
		Rows:    rows,
		Raw:     prices,
	})
}

func parseProductPriceFlags(values []string) ([]api.ProductPrice, error) {
	prices := make([]api.ProductPrice, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		price, err := parseProductPrice(value)
		if err != nil {
			return nil, err
		}
		if seen[price.Currency] {
			return nil, fmt.Errorf("duplicate price currency %s", price.Currency)
		}
		seen[price.Currency] = true
		prices = append(prices, price)
	}
	return prices, nil
}

func normalizeStoreFlag(value string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "_", "-")
}

func parseProductPrice(value string) (api.ProductPrice, error) {
	currency, amount, ok := strings.Cut(value, ":")
	if !ok {
		return api.ProductPrice{}, fmt.Errorf("--price must use CURRENCY:AMOUNT, e.g. USD:9.99")
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if len(currency) != 3 {
		return api.ProductPrice{}, fmt.Errorf("currency %q must be a 3-letter ISO code", currency)
	}
	for _, r := range currency {
		if !unicode.IsLetter(r) || r > unicode.MaxASCII {
			return api.ProductPrice{}, fmt.Errorf("currency %q must be a 3-letter ISO code", currency)
		}
	}
	micros, err := parseDecimalMicros(strings.TrimSpace(amount))
	if err != nil {
		return api.ProductPrice{}, fmt.Errorf("invalid price amount %q: %w", amount, err)
	}
	if micros <= 0 {
		return api.ProductPrice{}, fmt.Errorf("price amount must be greater than 0")
	}
	return api.ProductPrice{Currency: currency, AmountMicros: micros}, nil
}

func parseDecimalMicros(value string) (int64, error) {
	if value == "" {
		return 0, fmt.Errorf("empty amount")
	}
	if strings.HasPrefix(value, "+") {
		value = strings.TrimPrefix(value, "+")
	}
	if strings.HasPrefix(value, "-") {
		return 0, fmt.Errorf("negative amount")
	}
	whole, frac, hasFrac := strings.Cut(value, ".")
	if whole == "" {
		whole = "0"
	}
	if wholePart, err := strconv.ParseInt(whole, 10, 64); err != nil {
		return 0, err
	} else {
		if !hasFrac {
			return wholePart * 1_000_000, nil
		}
		if frac == "" {
			return wholePart * 1_000_000, nil
		}
		if len(frac) > 6 {
			return 0, fmt.Errorf("more than 6 decimal places")
		}
		for _, r := range frac {
			if r < '0' || r > '9' {
				return 0, fmt.Errorf("amount must be numeric")
			}
		}
		frac += strings.Repeat("0", 6-len(frac))
		fracPart, err := strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, err
		}
		return wholePart*1_000_000 + fracPart, nil
	}
}

func formatMicros(micros int64) string {
	whole := micros / 1_000_000
	frac := micros % 1_000_000
	if frac == 0 {
		return strconv.FormatInt(whole, 10)
	}
	s := fmt.Sprintf("%d.%06d", whole, frac)
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}
