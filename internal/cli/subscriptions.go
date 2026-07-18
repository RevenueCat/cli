package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

func newSubscriptionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "subscriptions",
		Aliases: []string{"subscription", "subs", "sub"},
		Short:   "Inspect and manage subscriptions",
	}
	cmd.AddCommand(
		newSubsShowCmd(),
		newSubsTransactionsCmd(),
		newSubsEntitlementsCmd(),
		newSubsManagementURLCmd(),
		newSubsCancelCmd(),
		newSubsExtendCmd(),
		newSubsRefundCmd(),
	)
	return cmd
}

func newSubsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show a subscription",
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
			s, err := client.Subscriptions.Get(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			if !rt.Globals.JSON && !rt.Globals.NoInput && tui.IsInteractive() {
				item := subscriptionToItem(cmd.Context(), client, projectID, s.CustomerID, *s)
				return tui.RunBrowser("Subscription", []tui.BrowserItem{item})
			}
			return rt.Out.Render(s)
		},
	}
}

func newSubsTransactionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "transactions <id>",
		Short: "List transactions for a subscription",
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
			page, err := client.Subscriptions.Transactions(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(page.Items))
			for _, t := range page.Items {
				rows = append(rows, []string{t.ID, formatMillis(t.PurchasedAt)})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "PURCHASED AT"},
				Rows:    rows,
				Raw:     page,
			})
		},
	}
}

func newSubsEntitlementsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "entitlements <id>",
		Short: "List entitlements granted by a subscription",
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
			page, err := client.Subscriptions.Entitlements(cmd.Context(), projectID, args[0])
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

func newSubsManagementURLCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "management-url <id>",
		Short: "Get the store-specific subscription management URL",
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
			mu, err := client.Subscriptions.ManagementURL(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			return rt.Out.Render(mu)
		},
	}
}

func newSubsCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <id>",
		Short: "Cancel a Web Billing subscription",
		Long: `Cancels a subscription. Web Billing only — App Store / Play Store
subscriptions must be cancelled through the store.

Reversibility: a cancelled subscription cannot be uncancelled via API; the
customer would need to start a new subscription.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc subscriptions cancel sub_abc --yes`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if err := confirmOrAbort(rt, fmt.Sprintf("Cancel subscription %q?", args[0])); err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			s, err := client.Subscriptions.Cancel(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Cancelled %s", args[0]))
			return rt.Out.Render(s)
		},
	}
}

func newSubsExtendCmd() *cobra.Command {
	var duration string
	cmd := &cobra.Command{
		Use:   "extend <id> --by <duration>",
		Short: "Extend a subscription's billing period",
		Long: `Extends a subscription's current period by an ISO 8601 duration.
Used for support gestures (free month after an outage, etc.).

Duration format: P[n]Y[n]M[n]D — e.g. P1M (one month), P7D (seven days),
P3M (three months).`,
		Example: `  rc subscriptions extend sub_abc --by P1M
  rc subscriptions extend sub_abc --by P7D --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if duration == "" {
				return fmt.Errorf("--by is required (ISO 8601 duration, e.g. P1M)")
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			s, err := client.Subscriptions.Extend(cmd.Context(), projectID, args[0], duration)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Extended %s by %s", args[0], duration))
			return rt.Out.Render(s)
		},
	}
	cmd.Flags().StringVar(&duration, "by", "", "ISO 8601 duration to extend by, e.g. P1M, P7D")
	return cmd
}

func newSubsRefundCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refund <id>",
		Short: "Refund a Web Billing subscription",
		Long: `Refunds the most recent payment on a Web Billing subscription. Web
Billing only — store-side refunds must be issued through the store.

Reversibility: irreversible. Money is returned to the customer's payment
method; recouping requires charging again.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc subscriptions refund sub_abc --yes`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if err := confirmOrAbort(rt, fmt.Sprintf("Refund subscription %q?", args[0])); err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			if err := client.Subscriptions.Refund(cmd.Context(), projectID, args[0]); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Refunded %s", args[0]))
			return rt.Out.Render(map[string]any{"ok": true, "id": args[0]})
		},
	}
}
