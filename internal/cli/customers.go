package cli

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/config"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

// grantExpiry translates a friendly promotional duration into the absolute
// expires_at (ms since epoch) the v2 grant endpoint requires. Durations are
// calendar-based (a month is a calendar month, not 30 days); "lifetime" maps to
// a far-future date since the endpoint has no null/forever expiry.
func grantExpiry(duration string, now time.Time) (int64, error) {
	var t time.Time
	switch duration {
	case "daily":
		t = now.AddDate(0, 0, 1)
	case "three_day":
		t = now.AddDate(0, 0, 3)
	case "weekly":
		t = now.AddDate(0, 0, 7)
	case "monthly":
		t = now.AddDate(0, 1, 0)
	case "two_month":
		t = now.AddDate(0, 2, 0)
	case "three_month":
		t = now.AddDate(0, 3, 0)
	case "six_month":
		t = now.AddDate(0, 6, 0)
	case "yearly":
		t = now.AddDate(1, 0, 0)
	case "lifetime":
		t = now.AddDate(100, 0, 0)
	default:
		return 0, fmt.Errorf("invalid duration %q: must be one of daily, three_day, weekly, monthly, two_month, three_month, six_month, yearly, lifetime", duration)
	}
	return t.UnixMilli(), nil
}

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
		newCustomerAliasesCmd(),
		newCustomerAttributesCmd(),
		newCustomerSetAttributeCmd(),
		newCustomerTransferCmd(),
		newCustomerOverrideOfferingCmd(),
		newCustomerClearOverrideCmd(),
		newCustomerRestoreGoogleCmd(),
		newCustomerWalletCmd(),
	)
	return cmd
}

func newCustomerAliasesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "aliases <customer-id>",
		Short: "List a customer's aliases",
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
			page, err := client.Customers.Aliases(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			return rt.Out.Render(page)
		},
	}
}

func newCustomerAttributesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attributes <customer-id>",
		Short: "List a customer's attributes",
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
			attrs, err := client.Customers.Attributes(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			return rt.Out.Render(attrs)
		},
	}
}

func newCustomerSetAttributeCmd() *cobra.Command {
	var sets []string
	cmd := &cobra.Command{
		Use:   "set-attribute <customer-id>",
		Short: "Set one or more attributes on a customer (--set key=value, repeatable)",
		Long: `Sets custom attributes on a customer. Pass --set key=value once per
attribute. Existing attributes with the same key are overwritten; others
are preserved.`,
		Example: `  rc customer set-attribute cus_abc --set email=user@example.com
  rc customer set-attribute cus_abc --set $segment=premium --set $churnRisk=low`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if len(sets) == 0 {
				return fmt.Errorf("at least one --set key=value is required")
			}
			attrs := map[string]string{}
			for _, s := range sets {
				k, v, ok := splitKV(s)
				if !ok {
					return fmt.Errorf("--set must be key=value, got %q", s)
				}
				attrs[k] = v
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			if err := client.Customers.SetAttributes(cmd.Context(), projectID, args[0], attrs); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Set %d attribute(s) on %s", len(attrs), args[0]))
			return rt.Out.Render(map[string]any{"ok": true, "attributes": attrs})
		},
	}
	cmd.Flags().StringArrayVar(&sets, "set", nil, "attribute key=value (repeatable)")
	return cmd
}

func splitKV(s string) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}

func newCustomerTransferCmd() *cobra.Command {
	var to string
	cmd := &cobra.Command{
		Use:   "transfer <source-customer-id> --to <dest-customer-id>",
		Short: "Transfer subscriptions and purchases from one customer to another",
		Long: `Transfers all subscriptions and purchases from a source customer to a
destination customer. Useful for merging duplicate customer records.

This is destructive on the source customer's purchase history; pass --yes
to skip the confirmation prompt.`,
		Example: `  rc customer transfer cus_old --to cus_new
  rc customer transfer cus_old --to cus_new --yes --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if to == "" {
				return fmt.Errorf("--to is required")
			}
			if !rt.Globals.AssumeYes {
				ok, err := tui.Confirm(rt.Globals.NoInput, fmt.Sprintf("Transfer %s -> %s?", args[0], to))
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
			if err := client.Customers.Transfer(cmd.Context(), projectID, args[0], to); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Transferred %s -> %s", args[0], to))
			return rt.Out.Render(map[string]any{"ok": true, "from": args[0], "to": to})
		},
	}
	cmd.Flags().StringVar(&to, "to", "", "destination customer ID (required)")
	return cmd
}

func newCustomerOverrideOfferingCmd() *cobra.Command {
	var offering string
	cmd := &cobra.Command{
		Use:   "override-offering <customer-id> --offering <id>",
		Short: "Assign an offering override to a customer",
		Long: `Forces a specific offering to be shown to one customer regardless of
which offering is currently the project default. Common for A/B tests or
support overrides. Use 'rc customer clear-override' to remove.`,
		Example: `  rc customer override-offering cus_abc --offering ofrng_promo_2026
  rc customer clear-override cus_abc`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if offering == "" {
				return fmt.Errorf("--offering is required (use `rc customer clear-override` to remove)")
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			if err := client.Customers.OverrideOffering(cmd.Context(), projectID, args[0], offering); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Set offering override %s for %s", offering, args[0]))
			return rt.Out.Render(map[string]any{"ok": true, "customer_id": args[0], "offering_id": offering})
		},
	}
	cmd.Flags().StringVar(&offering, "offering", "", "offering ID")
	return cmd
}

func newCustomerClearOverrideCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear-override <customer-id>",
		Short: "Clear a customer's offering override",
		Long: `Removes any offering override set via ` + "`rc customer override-offering`" + `.
The customer will see whichever offering is the project default.

Reversibility: re-apply with ` + "`rc customer override-offering`" + `.

Confirmation: no prompt.`,
		Example: `  rc customer clear-override cus_abc`,
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
			if err := client.Customers.OverrideOffering(cmd.Context(), projectID, args[0], ""); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Cleared override for %s", args[0]))
			return rt.Out.Render(map[string]any{"ok": true, "customer_id": args[0]})
		},
	}
}

func newCustomerRestoreGoogleCmd() *cobra.Command {
	var token string
	cmd := &cobra.Command{
		Use:   "restore-google <customer-id> --token <purchase-token>",
		Short: "Restore a Google Play purchase for a customer",
		Long: `Re-syncs a Google Play purchase to a customer using a Google Play
purchase token. Useful when a purchase was made on-device but didn't reach
RevenueCat (network failure, app uninstall mid-purchase, etc.).

Reversibility: the resulting subscription can be cancelled normally, but
the original token consumption with Google cannot be undone.

Confirmation: no prompt — idempotent (re-running with the same token is safe).`,
		Example: `  rc customer restore-google cus_abc --token GPA.xxxx-xxxx-xxxx-xxxxx`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if token == "" {
				return fmt.Errorf("--token is required")
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			if err := client.Customers.RestoreGooglePlay(cmd.Context(), projectID, args[0], token); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Restored Google Play purchase for %s", args[0]))
			return rt.Out.Render(map[string]any{"ok": true, "customer_id": args[0]})
		},
	}
	cmd.Flags().StringVar(&token, "token", "", "Google Play purchase token")
	return cmd
}

func newCustomerWalletCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "wallet <customer-id>",
		Short: "Show a customer's virtual currency balances",
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
			page, err := client.Customers.Wallet(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			return rt.Out.Render(page)
		},
	}
}

func requireProject(rt *Runtime) (string, error) {
	if rt.Config.ProjectID != "" {
		return rt.Config.ProjectID, nil
	}
	if rt.Globals.NoInput {
		return "", fmt.Errorf("no active project: run `rc projects use <id>` or pass --project-id")
	}
	return pickProjectInteractive(rt.Ctx, rt)
}

func pickProjectInteractive(ctx context.Context, rt *Runtime) (string, error) {
	client, err := rt.API()
	if err != nil {
		return "", err
	}
	page, err := client.Projects.List(ctx)
	if err != nil {
		return "", fmt.Errorf("fetching projects: %w", err)
	}
	if len(page.Items) == 0 {
		return "", fmt.Errorf("no projects found; create one at https://app.revenuecat.com")
	}
	if len(page.Items) == 1 {
		rt.Out.Info(fmt.Sprintf("Using project: %s (%s)", page.Items[0].Name, page.Items[0].ID))
		return page.Items[0].ID, nil
	}

	const noDefault = "__no_default__"
	projectOpts := make([]huh.Option[string], len(page.Items))
	for i, p := range page.Items {
		projectOpts[i] = huh.NewOption(fmt.Sprintf("%s  (%s)", p.Name, p.ID), p.ID)
	}
	allOpts := append([]huh.Option[string]{
		huh.NewOption("Ask me every time  (don't save a default)", noDefault),
	}, projectOpts...)

	var projectID string
	sel := huh.NewSelect[string]().
		Title("Select a project").
		Description("Type to filter  ·  Enter to confirm").
		Options(allOpts...).
		Filtering(true).
		Value(&projectID)
	if err := tui.Form(false).Field(sel).Run(); err != nil {
		return "", err
	}

	if projectID == noDefault {
		// Clear any saved default so future commands also prompt, then pick for this command.
		rt.Config.ProjectID = ""
		if err := config.Save(rt.Globals.Profile, rt.Config); err != nil {
			rt.Out.Info(fmt.Sprintf("note: couldn't save profile: %v", err))
		}

		var pick string
		pickSel := huh.NewSelect[string]().
			Title("Select a project for this command").
			Description("Type to filter  ·  Enter to confirm").
			Options(projectOpts...).
			Filtering(true).
			Value(&pick)
		if err := tui.Form(false).Field(pickSel).Run(); err != nil {
			return "", err
		}
		return pick, nil
	}

	rt.Config.ProjectID = projectID
	if err := config.Save(rt.Globals.Profile, rt.Config); err != nil {
		rt.Out.Info(fmt.Sprintf("note: couldn't save profile: %v", err))
	}
	return projectID, nil
}

func newCustomerListCmd() *cobra.Command {
	var limit int
	var cursor string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List customers in the active project",
		Long: `Lists customers, paginated. In TTY mode launches an interactive browser;
pass --json for machine-readable output or --no-input to disable the browser.`,
		Example: `  rc customer list
  rc customer list --json --limit 100 | jq '.data.items[].id'
  rc customer list --cursor cus_xyz --limit 50`,
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

			if !rt.Globals.JSON && !rt.Globals.NoInput && tui.IsInteractive() {
				cols := []string{"ID", "PLATFORM", "COUNTRY", "FIRST SEEN", "LAST SEEN"}
				return tui.RunBrowserTable("Customers", cols, customersToItems(cmd.Context(), client, projectID, page.Items))
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
	return &cobra.Command{
		Use:   "show <customer-id>",
		Short: "Show a complete view of a customer",
		Long: `Composes the customer record (which already embeds active entitlements),
subscriptions, and purchases into one envelope. In TTY mode launches an
interactive detail view with drill-down. Use --json for the raw merged document.`,
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
			id := args[0]
			customer, err := client.Customers.Get(cmd.Context(), projectID, id)
			if err != nil {
				return err
			}
			if !rt.Globals.JSON && !rt.Globals.NoInput && tui.IsInteractive() {
				item := customerToItem(cmd.Context(), client, projectID, *customer)
				return tui.RunBrowser("Customer", []tui.BrowserItem{item})
			}
			subs, subsErr := client.Customers.Subscriptions(cmd.Context(), projectID, id)
			purs, pursErr := client.Customers.Purchases(cmd.Context(), projectID, id)
			raw := map[string]any{
				"customer":      customer,
				"subscriptions": subs,
				"purchases":     purs,
			}
			if subsErr != nil {
				raw["subscriptions_error"] = subsErr.Error()
			}
			if pursErr != nil {
				raw["purchases_error"] = pursErr.Error()
			}
			return rt.Out.RenderCard(customerCard(customer, subs, purs, raw))
		},
	}
}

var grantDurationOptions = []huh.Option[string]{
	huh.NewOption("Daily", "daily"),
	huh.NewOption("Three day", "three_day"),
	huh.NewOption("Weekly", "weekly"),
	huh.NewOption("Monthly", "monthly"),
	huh.NewOption("Two month", "two_month"),
	huh.NewOption("Three month", "three_month"),
	huh.NewOption("Six month", "six_month"),
	huh.NewOption("Yearly", "yearly"),
	huh.NewOption("Lifetime", "lifetime"),
}

func newCustomerGrantCmd() *cobra.Command {
	var duration string
	cmd := &cobra.Command{
		Use:   "grant <customer-id> [entitlement-id]",
		Short: "Grant a promotional entitlement to a customer",
		Long: `Grants a promotional entitlement to a customer for a fixed duration.

customer-id is required. entitlement-id is optional under a TTY — omit it
to pick from the project's entitlement catalog interactively.

Duration must be one of: daily, three_day, weekly, monthly, two_month,
three_month, six_month, yearly, lifetime. Omit --duration under a TTY to
pick from the list interactively.`,
		Example: `  rc customer grant cus_abc                          # TTY: picks entitlement + duration
  rc customer grant cus_abc pro --duration monthly   # fully explicit
  rc customer grant cus_abc pro --duration monthly --yes --json`,
		Args: cobra.RangeArgs(1, 2),
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
			customerID := args[0]
			entitlementID, err := requireID(rt, argAt(args, 1), "entitlement", func() ([]PickerItem, error) {
				page, err := client.Entitlements.List(cmd.Context(), projectID)
				if err != nil {
					return nil, err
				}
				items := make([]PickerItem, len(page.Items))
				for i, e := range page.Items {
					label := e.LookupKey
					if e.DisplayName != "" {
						label = fmt.Sprintf("%s  (%s)", e.DisplayName, e.LookupKey)
					}
					items[i] = PickerItem{ID: e.ID, Label: label}
				}
				return items, nil
			})
			if err != nil {
				return err
			}
			if duration == "" {
				if rt.Globals.NoInput || !tui.IsInteractive() {
					return fmt.Errorf("--duration is required")
				}
				sel := huh.NewSelect[string]().Title("Duration").Options(grantDurationOptions...).Value(&duration)
				if err := tui.Form(false).Field(sel).Run(); err != nil {
					return err
				}
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
			expiresAt, err := grantExpiry(duration, time.Now())
			if err != nil {
				return err
			}
			result, err := client.Customers.GrantEntitlement(cmd.Context(), projectID, customerID, entitlementID, expiresAt)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Granted %s to %s (%s)", entitlementID, customerID, duration))
			return rt.Out.Render(result)
		},
	}
	cmd.Flags().StringVar(&duration, "duration", "", "duration: daily|three_day|weekly|monthly|two_month|three_month|six_month|yearly|lifetime")
	return cmd
}

func newCustomerRevokeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke <customer-id> [entitlement-id]",
		Short: "Revoke a promotional entitlement from a customer",
		Long: `Revokes a previously-granted promotional entitlement. Only affects
promotional grants made through ` + "`rc customer grant`" + ` — store
purchases are not affected.

customer-id is required. entitlement-id is optional under a TTY — omit it
to pick from the project's entitlement catalog interactively.

Reversibility: re-grant with ` + "`rc customer grant`" + ` if needed.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc customer revoke cus_abc              # TTY: picks entitlement
  rc customer revoke cus_abc pro --yes   # fully explicit`,
		Args: cobra.RangeArgs(1, 2),
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
			customerID := args[0]
			entitlementID, err := requireID(rt, argAt(args, 1), "entitlement", func() ([]PickerItem, error) {
				page, err := client.Entitlements.List(cmd.Context(), projectID)
				if err != nil {
					return nil, err
				}
				items := make([]PickerItem, len(page.Items))
				for i, e := range page.Items {
					label := e.LookupKey
					if e.DisplayName != "" {
						label = fmt.Sprintf("%s  (%s)", e.DisplayName, e.LookupKey)
					}
					items[i] = PickerItem{ID: e.ID, Label: label}
				}
				return items, nil
			})
			if err != nil {
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
			if err := client.Customers.RevokeEntitlement(cmd.Context(), projectID, customerID, entitlementID); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Revoked %s from %s", entitlementID, customerID))
			return rt.Out.Render(map[string]any{"ok": true})
		},
	}
	return cmd
}

// customerCard composes the pretty TTY view of `rc customer show`. JSON
// callers never touch this — they get `raw` straight through Render().
func customerCard(c *api.Customer, subs *api.Page[api.Subscription], purs *api.Page[api.Purchase], raw any) output.Card {
	card := output.Card{
		Title: c.ID,
		Raw:   raw,
	}
	if c.LastSeenPlatform != "" || c.LastSeenCountry != "" {
		card.Title += "  ·  " + nonEmpty(c.LastSeenPlatform, "—") + "  ·  " + nonEmpty(c.LastSeenCountry, "—")
	}
	first := formatMillis(c.FirstSeenAt)
	last := formatMillis(c.LastSeenAt)
	if first != "" || last != "" {
		card.Subtitle = fmt.Sprintf("first seen %s · last seen %s", nonEmpty(first, "—"), nonEmpty(last, "—"))
	}

	// Active entitlements as chips.
	entSection := output.CardSection{Heading: "Active entitlements", Empty: "no active entitlements"}
	if c.ActiveEntitlements != nil {
		for _, e := range c.ActiveEntitlements.Items {
			label := e.LookupKey
			if label == "" {
				label = e.ID
			}
			entSection.Chips = append(entSection.Chips, output.Chip{Label: label, Tone: output.ToneActive})
		}
	}
	card.Sections = append(card.Sections, entSection)

	// Subscriptions table.
	subSection := output.CardSection{Heading: "Subscriptions", Empty: "no subscriptions"}
	if subs != nil && len(subs.Items) > 0 {
		tab := &output.CardTable{Columns: []string{"ID", "PRODUCT", "STORE", "STATUS", "PERIOD ENDS"}}
		for _, s := range subs.Items {
			tab.Rows = append(tab.Rows, []string{
				s.ID,
				nonEmpty(s.ProductID, "—"),
				nonEmpty(s.Store, "—"),
				nonEmpty(s.Status, "—"),
				formatMillis(s.CurrentPeriodEnds),
			})
		}
		subSection.Table = tab
	}
	card.Sections = append(card.Sections, subSection)

	// Purchases (collapsed: just IDs as a comma-separated list when many).
	purSection := output.CardSection{Heading: "Purchases", Empty: "no purchases"}
	if purs != nil && len(purs.Items) > 0 {
		tab := &output.CardTable{Columns: []string{"ID", "PRODUCT", "STORE", "PURCHASED"}}
		for _, p := range purs.Items {
			tab.Rows = append(tab.Rows, []string{
				p.ID,
				nonEmpty(p.ProductID, "—"),
				nonEmpty(p.Store, "—"),
				formatMillis(p.PurchasedAt),
			})
		}
		purSection.Table = tab
	}
	card.Sections = append(card.Sections, purSection)

	return card
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// ── browser helpers ──────────────────────────────────────────────────────────

func customersToItems(ctx context.Context, client *api.Client, projectID string, customers []api.Customer) []tui.BrowserItem {
	items := make([]tui.BrowserItem, len(customers))
	for i, c := range customers {
		c := c
		items[i] = customerToItem(ctx, client, projectID, c)
	}
	return items
}

func customerToItem(ctx context.Context, client *api.Client, projectID string, c api.Customer) tui.BrowserItem {
	var metaParts []string
	if c.LastSeenPlatform != "" {
		metaParts = append(metaParts, c.LastSeenPlatform)
	}
	if c.LastSeenCountry != "" {
		metaParts = append(metaParts, c.LastSeenCountry)
	}
	customerURL := fmt.Sprintf("https://app.revenuecat.com/projects/%s/customers/%s", dashboardProjectID(projectID), c.ID)
	return tui.BrowserItem{
		ID:     c.ID,
		Label:  c.ID,
		Meta:   strings.Join(metaParts, " · "),
		Row:    []string{c.ID, c.LastSeenPlatform, c.LastSeenCountry, formatMillis(c.FirstSeenAt), formatMillis(c.LastSeenAt)},
		WebURL: customerURL,
		Fields: []tui.BrowserField{
			{Key: "ID", Value: c.ID},
			{Key: "Platform", Value: c.LastSeenPlatform},
			{Key: "Country", Value: c.LastSeenCountry},
			{Key: "App version", Value: c.LastSeenAppVersion},
			{Key: "First seen", Value: formatMillis(c.FirstSeenAt)},
			{Key: "Last seen", Value: formatMillis(c.LastSeenAt)},
		},
		AutoLoad: func() ([]tui.BrowserSection, error) {
			// Parallel fetch of all customer sub-resources.
			type results struct {
				ents *api.Page[api.Entitlement]
				subs *api.Page[api.Subscription]
				purs *api.Page[api.Purchase]
				invs *api.Page[api.Invoice]
			}
			var res results
			var mu sync.Mutex
			var wg sync.WaitGroup
			var firstErr error
			fetchErr := func(err error) {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
			wg.Add(4)
			go func() {
				defer wg.Done()
				p, err := client.Customers.ActiveEntitlements(ctx, projectID, c.ID)
				if err != nil {
					fetchErr(err)
					return
				}
				mu.Lock()
				res.ents = p
				mu.Unlock()
			}()
			go func() {
				defer wg.Done()
				p, err := client.Customers.Subscriptions(ctx, projectID, c.ID)
				if err != nil {
					fetchErr(err)
					return
				}
				mu.Lock()
				res.subs = p
				mu.Unlock()
			}()
			go func() {
				defer wg.Done()
				p, err := client.Customers.Purchases(ctx, projectID, c.ID)
				if err != nil {
					fetchErr(err)
					return
				}
				mu.Lock()
				res.purs = p
				mu.Unlock()
			}()
			go func() {
				defer wg.Done()
				p, err := client.Invoices.ListForCustomer(ctx, projectID, c.ID)
				if err != nil {
					fetchErr(err)
					return
				}
				mu.Lock()
				res.invs = p
				mu.Unlock()
			}()
			wg.Wait()
			// Partial results still render (their sections show data), but if
			// every fetch failed surface the error instead of an empty view.
			if firstErr != nil && res.ents == nil && res.subs == nil && res.purs == nil && res.invs == nil {
				return nil, firstErr
			}

			var sections []tui.BrowserSection

			// Active entitlements — selectable, drills to entitlement detail
			sec0 := tui.BrowserSection{Title: "Active Entitlements", Cols: []string{"LOOKUP KEY", "SOURCE", "EXPIRES"}, Empty: "no active entitlements"}
			if res.ents != nil {
				for _, e := range res.ents.Items {
					e := e
					item := entitlementToItem(ctx, client, projectID, e)
					sec0.Rows = append(sec0.Rows, tui.BrowserSectionRow{
						Cells: []string{nonEmpty(e.LookupKey, e.ID), e.Source, formatMillis(e.ExpiresAt)},
						Item:  &item,
					})
				}
			}
			sections = append(sections, sec0)

			// Subscriptions — selectable, drills to subscription detail
			sec1 := tui.BrowserSection{Title: "Subscriptions", Cols: []string{"PRODUCT", "STORE", "STATUS", "PERIOD ENDS"}, Empty: "no subscriptions"}
			if res.subs != nil {
				for _, s := range res.subs.Items {
					s := s
					item := subscriptionToItem(ctx, client, projectID, c.ID, s)
					sec1.Rows = append(sec1.Rows, tui.BrowserSectionRow{
						Cells: []string{s.ProductID, s.Store, s.Status, formatMillis(s.CurrentPeriodEnds)},
						Item:  &item,
					})
				}
			}
			sections = append(sections, sec1)

			// Purchases — selectable, drills to purchase detail
			sec2 := tui.BrowserSection{Title: "Purchases", Cols: []string{"PRODUCT", "STORE", "PURCHASED"}, Empty: "no purchases"}
			if res.purs != nil {
				for _, p := range res.purs.Items {
					p := p
					item := purchaseToItem(ctx, client, projectID, c.ID, p)
					sec2.Rows = append(sec2.Rows, tui.BrowserSectionRow{
						Cells: []string{p.ProductID, p.Store, formatMillis(p.PurchasedAt)},
						Item:  &item,
					})
				}
			}
			sections = append(sections, sec2)

			// Invoices — display only
			sec3 := tui.BrowserSection{Title: "Invoices", Cols: []string{"ID", "ISSUED", "STATUS"}, Empty: "no invoices"}
			if res.invs != nil {
				for _, inv := range res.invs.Items {
					sec3.Rows = append(sec3.Rows, tui.BrowserSectionRow{
						Cells: []string{inv.ID, formatMillis(inv.IssuedAt), inv.Status},
					})
				}
			}
			sections = append(sections, sec3)

			return sections, nil
		},
	}
}

// subscriptionToItem builds a detail item for a subscription, with inline
// transactions and entitlements loaded via AutoLoad.
func subscriptionToItem(ctx context.Context, client *api.Client, projectID, customerID string, s api.Subscription) tui.BrowserItem {
	givesAccess := "no"
	if s.GivesAccess {
		givesAccess = "yes"
	}
	return tui.BrowserItem{
		ID:     s.ID,
		Label:  s.ProductID,
		Meta:   s.Status,
		WebURL: fmt.Sprintf("https://app.revenuecat.com/projects/%s/customers/%s", dashboardProjectID(projectID), customerID),
		Fields: []tui.BrowserField{
			{Key: "ID", Value: s.ID},
			{Key: "Product", Value: s.ProductID},
			{Key: "Store", Value: s.Store},
			{Key: "Status", Value: s.Status},
			{Key: "Auto-renewal", Value: s.AutoRenewalStatus},
			{Key: "Gives access", Value: givesAccess},
			{Key: "Starts", Value: formatMillis(s.StartsAt)},
			{Key: "Period ends", Value: formatMillis(s.CurrentPeriodEnds)},
		},
		AutoLoad: func() ([]tui.BrowserSection, error) {
			var sections []tui.BrowserSection

			// Transactions — display only (no sub-detail for a transaction)
			txPage, txErr := client.Subscriptions.Transactions(ctx, projectID, s.ID)
			if txErr == nil {
				sec := tui.BrowserSection{Title: "Transactions", Cols: []string{"ID", "PURCHASED", "REVENUE USD"}, Empty: "no transactions"}
				for _, t := range txPage.Items {
					rev := ""
					if t.RevenueInUSD != nil {
						rev = fmt.Sprintf("%v", t.RevenueInUSD)
					}
					sec.Rows = append(sec.Rows, tui.BrowserSectionRow{
						Cells: []string{t.ID, formatMillis(t.PurchasedAt), rev},
					})
				}
				sections = append(sections, sec)
			}

			// Entitlements — selectable, drills to entitlement detail
			entPage, entErr := client.Subscriptions.Entitlements(ctx, projectID, s.ID)
			if entErr == nil {
				sec := tui.BrowserSection{Title: "Entitlements", Cols: []string{"LOOKUP KEY", "SOURCE"}, Empty: "no entitlements"}
				for _, e := range entPage.Items {
					e := e
					item := entitlementToItem(ctx, client, projectID, e)
					sec.Rows = append(sec.Rows, tui.BrowserSectionRow{
						Cells: []string{nonEmpty(e.LookupKey, e.ID), e.Source},
						Item:  &item,
					})
				}
				sections = append(sections, sec)
			}

			// Partial results still render; only a total failure surfaces.
			if txErr != nil && entErr != nil {
				return nil, txErr
			}
			return sections, nil
		},
	}
}

// purchaseToItem builds a detail item for a one-time purchase.
func purchaseToItem(ctx context.Context, client *api.Client, projectID, customerID string, p api.Purchase) tui.BrowserItem {
	return tui.BrowserItem{
		ID:     p.ID,
		Label:  p.ProductID,
		Meta:   p.Store,
		WebURL: fmt.Sprintf("https://app.revenuecat.com/projects/%s/customers/%s", dashboardProjectID(projectID), customerID),
		Fields: []tui.BrowserField{
			{Key: "ID", Value: p.ID},
			{Key: "Product", Value: p.ProductID},
			{Key: "Store", Value: p.Store},
			{Key: "Purchased", Value: formatMillis(p.PurchasedAt)},
		},
		AutoLoad: func() ([]tui.BrowserSection, error) {
			entPage, err := client.Purchases.Entitlements(ctx, projectID, p.ID)
			if err != nil {
				return nil, err
			}
			sec := tui.BrowserSection{Title: "Entitlements", Cols: []string{"LOOKUP KEY", "SOURCE"}, Empty: "no entitlements"}
			for _, e := range entPage.Items {
				e := e
				item := entitlementToItem(ctx, client, projectID, e)
				sec.Rows = append(sec.Rows, tui.BrowserSectionRow{
					Cells: []string{nonEmpty(e.LookupKey, e.ID), e.Source},
					Item:  &item,
				})
			}
			return []tui.BrowserSection{sec}, nil
		},
	}
}
