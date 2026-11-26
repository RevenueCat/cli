package cli

import (
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
)

func newCustomerSubscriptionsCmd() *cobra.Command {
	var opts api.ListOptions
	cmd := &cobra.Command{
		Use:   "subscriptions <customer-id>",
		Short: "List a Customer's Subscriptions",
		Example: `  rc customers subscriptions cus_abc --limit 25
  rc customers subscriptions cus_abc --cursor sub_xyz --limit 25 --json`,
		Args: cobra.ExactArgs(1),
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
			page, err := client.Customers.Subscriptions(cmd.Context(), projectID, args[0], &opts)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(page.Items))
			for _, subscription := range page.Items {
				rows = append(rows, []string{
					subscription.ID,
					derefStr(subscription.ProductID),
					string(subscription.Store),
					string(subscription.Status),
					formatMillisPtr(subscription.CurrentPeriodEndsAt),
				})
			}
			if err := rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "PRODUCT", "STORE", "STATUS", "PERIOD ENDS"},
				Rows:    rows,
				Raw:     page,
			}); err != nil {
				return err
			}
			hintMoreResults(rt, page)
			return nil
		},
	}
	addListPaginationFlags(cmd, &opts)
	return cmd
}

func newCustomerPurchasesCmd() *cobra.Command {
	var opts api.ListOptions
	cmd := &cobra.Command{
		Use:   "purchases <customer-id>",
		Short: "List a Customer's Non-Subscription Purchases",
		Example: `  rc customers purchases cus_abc --limit 25
  rc customers purchases cus_abc --cursor pur_xyz --limit 25 --json`,
		Args: cobra.ExactArgs(1),
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
			page, err := client.Customers.Purchases(cmd.Context(), projectID, args[0], &opts)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(page.Items))
			for _, purchase := range page.Items {
				rows = append(rows, []string{
					purchase.ID,
					purchase.ProductID,
					string(purchase.Store),
					formatMillis(purchase.PurchasedAt),
				})
			}
			if err := rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "PRODUCT", "STORE", "PURCHASED"},
				Rows:    rows,
				Raw:     page,
			}); err != nil {
				return err
			}
			hintMoreResults(rt, page)
			return nil
		},
	}
	addListPaginationFlags(cmd, &opts)
	return cmd
}
