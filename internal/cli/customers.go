package cli

import (
	"context"
	"fmt"
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
		Long: `Lists customers, paginated. The TTY view prints a hint with the cursor
ID for the next page; the --json view returns the next_page URL in the page
envelope so agents can iterate without re-parsing.`,
		Example: `  rc customer list --limit 10
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
			// Best-effort enrichment; partial errors are surfaced in the envelope
			// so a JSON consumer sees what's missing rather than silently getting nil.
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
	cmd.Flags().StringVar(&id, "id", "", "customer ID")
	return cmd
}

func newCustomerGrantCmd() *cobra.Command {
	var customerID, entitlementID, duration string
	cmd := &cobra.Command{
		Use:   "grant",
		Short: "Grant a promotional entitlement to a customer",
		Long: `Grants a promotional entitlement to a customer for a fixed duration.

Under a TTY, missing inputs are prompted. Under --no-input, missing required
inputs return a usage error. Confirmation is skipped if --yes is set.

Duration must be one of: daily, three_day, weekly, monthly, two_month,
three_month, six_month, yearly, lifetime.`,
		Example: `  # Interactive (prompts for each field)
  rc customer grant

  # Non-interactive, scriptable
  rc customer grant --customer-id cus_abc --entitlement-id pro --duration monthly --yes

  # Agent-friendly
  rc customer grant --customer-id cus_abc --entitlement-id pro --duration monthly --yes --json`,
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

			expiresAt, err := grantExpiry(duration, time.Now())
			if err != nil {
				return err
			}
			client, err := rt.API()
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
		Long: `Revokes a previously-granted promotional entitlement. Only affects
promotional grants made through ` + "`rc customer grant`" + ` — store
purchases are not affected.

Reversibility: re-grant with ` + "`rc customer grant`" + ` if needed.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc customer revoke --customer-id cus_abc --entitlement-id pro --yes`,
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
