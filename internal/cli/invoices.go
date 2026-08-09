package cli

import (
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/output"
)

func newInvoicesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "invoices",
		Aliases: []string{"invoice"},
		Short:   "Inspect Web Billing invoices",
		Long: `Inspect Web Billing (formerly RevenueCat Billing) invoices — the billing records for
web purchases, with payment status and a downloadable PDF. These are
Web Billing records, not App Store or Play Store purchases.`,
		Example: `  rc invoices for cus_abc
  rc invoices show inv_abc`,
	}
	cmd.AddCommand(
		newInvoicesShowCmd(),
		newInvoicesForCustomerCmd(),
	)
	return cmd
}

func newInvoicesShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "show <id>",
		Short:   "Show a Web Billing invoice",
		Long:    `Shows a Web Billing invoice: its payment status, amount, issue date, and the URL to the downloadable PDF.`,
		Example: "  rc invoices show inv_abc\n  rc invoices show inv_abc --json | jq -r '.pdf_url'",
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
			inv, err := client.Invoices.Get(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			return rt.Out.Render(inv)
		},
	}
}

// `rc invoices for <customer-id>` is more user-friendly than `rc customer invoices`
// because invoices are an independent noun; users go looking for an invoice
// first and only narrow by customer if needed.
func newInvoicesForCustomerCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "for <customer-id>",
		Short:   "List a Customer's Web Billing invoices",
		Long:    `Lists the Web Billing invoices for a Customer by App User ID, with each invoice's payment status and issue date.`,
		Example: "  rc invoices for cus_abc\n  rc invoices for cus_abc --json | jq '.data.items[] | select(.status==\"paid\")'",
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
			page, err := client.Invoices.ListForCustomer(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(page.Items))
			for _, inv := range page.Items {
				rows = append(rows, []string{inv.ID, inv.Status, formatMillis(int64(inv.IssuedAt))})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "STATUS", "ISSUED"},
				Rows:    rows,
				Raw:     page,
			})
		},
	}
}
