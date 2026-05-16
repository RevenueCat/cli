package cli

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

// Customer commands illustrate the design principle: the CLI shape is NOT a
// 1:1 mirror of the REST API. `rc customer show` composes the customer record
// (which already embeds active_entitlements) with subscriptions and purchases
// to build one user-intent view.

func newCustomersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "customer",
		Aliases: []string{"customers"},
		Short:   "Inspect and manage customers",
	}
	cmd.AddCommand(
		newCustomerListCmd(),
		newCustomerShowCmd(),
		newCustomerGrantCmd(),
		newCustomerRevokeCmd(),
	)
	return cmd
}

func requireProject(rt *Runtime) (string, error) {
	if rt.Config.ProjectID == "" {
		return "", fmt.Errorf("no active project: run `rc projects use <id>` or pass --project-id")
	}
	return rt.Config.ProjectID, nil
}

func newCustomerListCmd() *cobra.Command {
	var limit int
	var cursor string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List customers in the active project",
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
			page, err := client.Customers.List(cmd.Context(), projectID, &api.ListCustomersOptions{
				Limit:         limit,
				StartingAfter: cursor,
			})
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(page.Items))
			for _, c := range page.Items {
				rows = append(rows, []string{
					c.ID,
					c.LastSeenPlatform,
					c.LastSeenCountry,
					formatMillis(c.FirstSeenAt),
					formatMillis(c.LastSeenAt),
				})
			}
			if err := rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "PLATFORM", "COUNTRY", "FIRST SEEN", "LAST SEEN"},
				Rows:    rows,
				Raw:     page,
			}); err != nil {
				return err
			}
			if page.NextPage != "" && !rt.Globals.JSON {
				rt.Out.Info(fmt.Sprintf("more results — pass --cursor %s for the next page", lastID(page.Items)))
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "max results per page (server default if unset)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "customer ID to start after (pagination)")
	return cmd
}

func lastID(items []api.Customer) string {
	if len(items) == 0 {
		return ""
	}
	return items[len(items)-1].ID
}

func newCustomerShowCmd() *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use:   "show [customer-id]",
		Short: "Show a complete view of a customer",
		Long: `Composes the customer record (which already embeds active entitlements),
subscriptions, and purchases into one envelope. Use --json for the raw merged
document.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			if len(args) == 1 {
				id = args[0]
			}
			if err := tui.Form(rt.Globals.NoInput).
				Field(huh.NewInput().Title("Customer ID").Value(&id).Validate(tui.Required("customer ID"))).
				Run(); err != nil {
				return err
			}

			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}

			customer, err := client.Customers.Get(cmd.Context(), projectID, id)
			if err != nil {
				return err
			}
			// Best-effort enrichment; surface partial data instead of failing if
			// one of the subresources errors (e.g. plan limits).
			var subs *api.Page[api.Subscription]
			var purs *api.Page[api.Purchase]
			subs, _ = client.Customers.Subscriptions(cmd.Context(), projectID, id)
			purs, _ = client.Customers.Purchases(cmd.Context(), projectID, id)

			return rt.Out.Render(map[string]any{
				"customer":      customer,
				"subscriptions": subs,
				"purchases":     purs,
			})
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "customer ID")
	return cmd
}

func newCustomerGrantCmd() *cobra.Command {
	var customerID, entitlementID, duration string
	cmd := &cobra.Command{
		Use:   "grant",
		Short: "Grant a promotional entitlement to a customer",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}

			if err := tui.Form(rt.Globals.NoInput).
				Field(huh.NewInput().Title("Customer ID").Value(&customerID).Validate(tui.Required("customer ID"))).
				Field(huh.NewInput().Title("Entitlement ID").Value(&entitlementID).Validate(tui.Required("entitlement ID"))).
				Field(huh.NewSelect[string]().
					Title("Duration").
					Options(
						huh.NewOption("Daily", "daily"),
						huh.NewOption("Three day", "three_day"),
						huh.NewOption("Weekly", "weekly"),
						huh.NewOption("Monthly", "monthly"),
						huh.NewOption("Two month", "two_month"),
						huh.NewOption("Three month", "three_month"),
						huh.NewOption("Six month", "six_month"),
						huh.NewOption("Yearly", "yearly"),
						huh.NewOption("Lifetime", "lifetime"),
					).
					Value(&duration)).
				Run(); err != nil {
				return err
			}

			if !rt.Globals.AssumeYes {
				ok, err := tui.Confirm(rt.Globals.NoInput,
					fmt.Sprintf("Grant %q to customer %q (%s)?", entitlementID, customerID, duration))
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
			result, err := client.Customers.GrantEntitlement(cmd.Context(), projectID, customerID, entitlementID, duration)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Granted %s to %s (%s)", entitlementID, customerID, duration))
			return rt.Out.Render(result)
		},
	}
	cmd.Flags().StringVar(&customerID, "customer-id", "", "customer ID")
	cmd.Flags().StringVar(&entitlementID, "entitlement-id", "", "entitlement ID")
	cmd.Flags().StringVar(&duration, "duration", "", "duration: daily|three_day|weekly|monthly|two_month|three_month|six_month|yearly|lifetime")
	return cmd
}

func newCustomerRevokeCmd() *cobra.Command {
	var customerID, entitlementID string
	cmd := &cobra.Command{
		Use:   "revoke",
		Short: "Revoke a promotional entitlement from a customer",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}

			if err := tui.Form(rt.Globals.NoInput).
				Field(huh.NewInput().Title("Customer ID").Value(&customerID).Validate(tui.Required("customer ID"))).
				Field(huh.NewInput().Title("Entitlement ID").Value(&entitlementID).Validate(tui.Required("entitlement ID"))).
				Run(); err != nil {
				return err
			}

			if !rt.Globals.AssumeYes {
				ok, err := tui.Confirm(rt.Globals.NoInput,
					fmt.Sprintf("Revoke %q from customer %q?", entitlementID, customerID))
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
			if err := client.Customers.RevokeEntitlement(cmd.Context(), projectID, customerID, entitlementID); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Revoked %s from %s", entitlementID, customerID))
			return rt.Out.Render(map[string]any{"ok": true})
		},
	}
	cmd.Flags().StringVar(&customerID, "customer-id", "", "customer ID")
	cmd.Flags().StringVar(&entitlementID, "entitlement-id", "", "entitlement ID")
	return cmd
}
