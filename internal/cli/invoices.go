package cli

import (
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/output"
)

func newInvoicesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "invoices",
		Aliases: []string{"invoice"},
		Short:   "Inspect invoices",
	}
	cmd.AddCommand(
		newInvoicesShowCmd(),
		newInvoicesForCustomerCmd(),
	)
	return cmd
}

func newInvoicesShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show an invoice",
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
		Use:   "for <customer-id>",
		Short: "List invoices for a customer",
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
			page, err := client.Invoices.ListForCustomer(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(page.Items))
			for _, inv := range page.Items {
				rows = append(rows, []string{inv.ID, inv.Status, formatMillis(inv.IssuedAt)})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "STATUS", "ISSUED"},
				Rows:    rows,
				Raw:     page,
			})
		},
	}
}
