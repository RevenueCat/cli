package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

// purchases.go — show opens the TUI browser in TTY mode.

func newPurchasesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "purchases",
		Aliases: []string{"purchase"},
		Short:   "Inspect and refund Non-Subscription Purchases",
		Long: `Inspect and refund Non-Subscription Purchases — one-time, consumable,
non-consumable, and lifetime purchases. View a purchase, list the Entitlements
it grants, or refund it. For recurring subscriptions use 'rc subscriptions'.`,
		Example: `  rc purchases show pur_abc
  rc purchases refund pur_abc --yes`,
	}
	cmd.AddCommand(
		newPurchasesShowCmd(),
		newPurchasesEntitlementsCmd(),
		newPurchasesRefundCmd(),
	)
	return cmd
}

func newPurchasesShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "show <id>",
		Short:   "Show a purchase",
		Long:    `Shows a Non-Subscription Purchase: its Product, store, purchase time, and the Customer (App User ID) it belongs to. In a terminal this opens the interactive browser.`,
		Example: "  rc purchases show pur_abc\n  rc purchases show pur_abc --json | jq '.store'",
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
			p, err := client.Purchases.Get(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			if !rt.Globals.JSON && !rt.Globals.NoInput && tui.IsInteractive() {
				item := purchaseToItem(cmd.Context(), client, projectID, p.CustomerID, *p)
				return tui.RunBrowser("Purchase", []tui.BrowserItem{item})
			}
			return rt.Out.Render(p)
		},
	}
}

func newPurchasesEntitlementsCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "entitlements <id>",
		Short:   "List Entitlements granted by a purchase",
		Long:    `Lists the Entitlements a Non-Subscription Purchase grants the Customer — lifetime access, for example.`,
		Example: "  rc purchases entitlements pur_abc\n  rc purchases entitlements pur_abc --json | jq '.data.items[].lookup_key'",
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
			page, err := client.Purchases.Entitlements(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(page.Items))
			for _, e := range page.Items {
				rows = append(rows, []string{e.ID, e.LookupKey, e.DisplayName})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "LOOKUP KEY", "DISPLAY NAME"},
				Rows:    rows,
				Raw:     page,
			})
		},
	}
}

func newPurchasesRefundCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refund <id>",
		Short: "Refund a Non-Subscription Purchase",
		Long: `Refunds a Non-Subscription Purchase and emits a CANCELLATION, removing any
Entitlement access it granted. Only Google Play and RevenueCat Billing
purchases can be refunded directly; App Store refunds must be issued through
Apple.

Reversibility: irreversible. Money is returned to the Customer's payment
method.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc purchases refund pur_abc --yes`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if err := confirmOrAbort(rt, fmt.Sprintf("Refund purchase %q?", args[0])); err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			if err := client.Purchases.Refund(cmd.Context(), projectID, args[0]); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Refunded %s", args[0]))
			return rt.Out.Render(map[string]any{"ok": true, "id": args[0]})
		},
	}
}
