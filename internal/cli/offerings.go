package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

func newOfferingsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "offerings",
		Aliases: []string{"offering", "offer"},
		Short:   "Manage offerings (and their packages)",
	}
	cmd.AddCommand(
		newOfferingsListCmd(),
		newOfferingsShowCmd(),
		newOfferingsVerifyCmd(),
		newOfferingsPreviewCmd(),
		newOfferingsCreateCmd(),
		newOfferingsUpdateCmd(),
		newOfferingsSetCurrentCmd(),
		newOfferingsDeleteCmd(),
		newOfferingsArchiveCmd(),
		newOfferingsRestoreCmd(),
		newOfferingsPackagesCmd(),
	)
	return cmd
}

type offeringVerification struct {
	Offering     api.Offering          `json:"offering"`
	Packages     []verifiedPackage     `json:"packages"`
	Paywalls     []api.Paywall         `json:"paywalls"`
	Entitlements []verifiedEntitlement `json:"entitlements"`
	Issues       []string              `json:"issues"`
}

type verifiedPackage struct {
	Package  api.Package       `json:"package"`
	Products []verifiedProduct `json:"products"`
}

type verifiedProduct struct {
	Product             api.Product             `json:"product"`
	EligibilityCriteria api.EligibilityCriteria `json:"eligibility_criteria"`
	Prices              []api.ProductPrice      `json:"prices,omitempty"`
	PriceError          string                  `json:"price_error,omitempty"`
}

type verifiedEntitlement struct {
	Entitlement api.Entitlement `json:"entitlement"`
	Products    []api.Product   `json:"products"`
}

func newOfferingsVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify [id]",
		Short: "Verify the complete configuration served by an offering",
		Long: `Builds one read-only graph containing the offering, its packages, attached
products and prices, matching entitlements, and attached paywall publication state.
The issues array calls out missing links that would prevent a working purchase flow.`,
		Example: `  rc offerings verify ofrng_default --json --no-input`,
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
			offeringID, err := requireID(rt, argAt(args, 0), "offering", func() ([]PickerItem, error) {
				return offeringPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			verification, err := verifyOffering(cmd.Context(), client, projectID, offeringID)
			if err != nil {
				return err
			}
			return rt.Out.Render(verification)
		},
	}
}

func verifyOffering(ctx context.Context, client *api.Client, projectID, offeringID string) (*offeringVerification, error) {
	offering, err := client.Offerings.Get(ctx, projectID, offeringID)
	if err != nil {
		return nil, err
	}
	result := &offeringVerification{Offering: *offering, Packages: []verifiedPackage{}, Paywalls: []api.Paywall{}, Entitlements: []verifiedEntitlement{}, Issues: []string{}}
	productIDs := map[string]bool{}

	packages, err := client.Packages.List(ctx, projectID, offeringID)
	if err != nil {
		return nil, err
	}
	if len(packages.Items) == 0 {
		result.Issues = append(result.Issues, "offering has no packages")
	}
	for _, pkg := range packages.Items {
		entry := verifiedPackage{Package: pkg, Products: []verifiedProduct{}}
		associations, err := client.Packages.ListProducts(ctx, projectID, pkg.ID)
		if err != nil {
			return nil, err
		}
		if len(associations.Items) == 0 {
			result.Issues = append(result.Issues, fmt.Sprintf("package %s has no attached products", pkg.ID))
		}
		for _, association := range associations.Items {
			productIDs[association.Product.ID] = true
			product := verifiedProduct{Product: association.Product, EligibilityCriteria: association.EligibilityCriteria}
			prices, priceErr := client.Products.ListPrices(ctx, projectID, association.Product.ID)
			if priceErr != nil {
				product.PriceError = priceErr.Error()
			} else {
				product.Prices = prices
			}
			entry.Products = append(entry.Products, product)
		}
		result.Packages = append(result.Packages, entry)
	}

	paywalls, err := client.Paywalls.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, paywall := range paywalls.Items {
		if paywall.OfferingID == offeringID {
			result.Paywalls = append(result.Paywalls, paywall)
			if paywall.PublishedAt == nil {
				result.Issues = append(result.Issues, fmt.Sprintf("paywall %s is still a draft", paywall.ID))
			}
		}
	}
	if len(result.Paywalls) == 0 {
		result.Issues = append(result.Issues, "offering has no attached paywall")
	}

	entitlements, err := client.Entitlements.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	coveredProducts := map[string]bool{}
	for _, entitlement := range entitlements.Items {
		products, err := client.Entitlements.ListProducts(ctx, projectID, entitlement.ID)
		if err != nil {
			return nil, err
		}
		entry := verifiedEntitlement{Entitlement: entitlement, Products: []api.Product{}}
		for _, product := range products.Items {
			if productIDs[product.ID] {
				entry.Products = append(entry.Products, product)
				coveredProducts[product.ID] = true
			}
		}
		if len(entry.Products) > 0 {
			result.Entitlements = append(result.Entitlements, entry)
		}
	}
	for productID := range productIDs {
		if !coveredProducts[productID] {
			result.Issues = append(result.Issues, fmt.Sprintf("product %s is not attached to an entitlement", productID))
		}
	}
	return result, nil
}

func newOfferingsPreviewCmd() *cobra.Command {
	var appUserID, publicAPIKey string
	appUserIDDefault := os.Getenv("RC_APP_USER_ID")
	if appUserIDDefault == "" {
		appUserIDDefault = "rc-cli-preview"
	}
	cmd := &cobra.Command{
		Use:   "preview [app-id]",
		Short: "Fetch the offerings payload exactly as an SDK sees it",
		Long: `Calls the RevenueCat v1 SDK offerings endpoint with an app's public SDK key.
Use this to verify the current offering, package-product mapping, and published
paywall components. A null paywall_components value means the SDK is receiving
the fallback rather than a published dashboard paywall.`,
		Example: `  rc offerings preview app_test --json --no-input
  rc offerings preview app_ios --public-api-key appl_... --app-user-id preview-user --json --no-input`,
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
			appID, err := requireID(rt, argAt(args, 0), "app", func() ([]PickerItem, error) {
				apps, err := client.Apps.List(cmd.Context(), projectID)
				if err != nil {
					return nil, err
				}
				items := make([]PickerItem, len(apps.Items))
				for i, app := range apps.Items {
					items[i] = PickerItem{ID: app.ID, Label: fmt.Sprintf("%s  (%s)", app.Name, app.Type)}
				}
				return items, nil
			})
			if err != nil {
				return err
			}
			if publicAPIKey == "" {
				keys, err := client.Apps.PublicAPIKeys(cmd.Context(), projectID, appID)
				if err != nil {
					return err
				}
				if len(keys.Items) != 1 {
					return fmt.Errorf("app has %d public SDK keys; pass --public-api-key to choose one", len(keys.Items))
				}
				publicAPIKey = keys.Items[0].Key
			}
			sdk := api.NewSDKService(rt.Config.BaseURL, nil, userAgent(rt.Globals.Version))
			raw, err := sdk.Offerings(cmd.Context(), publicAPIKey, appUserID)
			if err != nil {
				return err
			}
			var payload any
			if err := json.Unmarshal(raw, &payload); err != nil {
				return fmt.Errorf("decoding SDK offerings response: %w", err)
			}
			return rt.Out.RenderJSON(payload)
		},
	}
	cmd.Flags().StringVar(&appUserID, "app-user-id", appUserIDDefault, "app user ID sent to the SDK endpoint (or RC_APP_USER_ID)")
	cmd.Flags().StringVar(&publicAPIKey, "public-api-key", os.Getenv("RC_PUBLIC_API_KEY"), "public SDK API key; auto-selected when the app has one (or RC_PUBLIC_API_KEY)")
	return cmd
}

func newOfferingsSetCurrentCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-current [id]",
		Short: "Make an offering the current SDK offering",
		Long: `Makes this offering the project's current offering. Apps that request the
current offering from a RevenueCat SDK will receive this offering.

Confirmation is required because this changes the catalog served to customers.
Pass --yes for agents and non-interactive use. The change can be reversed by
setting a different offering as current.`,
		Example: `  rc offerings set-current ofrng_default
  rc offerings set-current ofrng_default --yes --json --no-input`,
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
			offeringID, err := requireID(rt, argAt(args, 0), "offering", func() ([]PickerItem, error) {
				return offeringPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			if err := confirmOrAbort(rt, fmt.Sprintf("Make offering %q current?", offeringID)); err != nil {
				return err
			}
			current := true
			offering, err := client.Offerings.Update(cmd.Context(), projectID, offeringID, api.OfferingUpdate{IsCurrent: &current})
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Current offering: %s", offering.ID))
			return rt.Out.Render(offering)
		},
	}
}

func newOfferingsArchiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "archive [id]",
		Short: "Archive an offering",
		Long: `Archives an offering so it stops being served to new customers while
existing subscribers keep their access.

Reversibility: use ` + "`rc offerings restore <id>`" + ` to undo.

Confirmation: no prompt — soft, reversible state change.`,
		Example: `  rc offerings archive ofrng_q1_promo`,
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
			offeringID, err := requireID(rt, argAt(args, 0), "offering", func() ([]PickerItem, error) {
				return offeringPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			o, err := client.Offerings.Archive(cmd.Context(), projectID, offeringID)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Archived %s", o.ID))
			return rt.Out.Render(o)
		},
	}
}

func newOfferingsRestoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore [id]",
		Short: "Restore an archived offering (= unarchive)",
		Long: `Restores a previously-archived offering. Inverse of
` + "`rc offerings archive`" + `.

Reversibility: re-archive with ` + "`rc offerings archive <id>`" + `.

Confirmation: no prompt.`,
		Example: `  rc offerings restore ofrng_q1_promo`,
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
			offeringID, err := requireID(rt, argAt(args, 0), "offering", func() ([]PickerItem, error) {
				return offeringPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			o, err := client.Offerings.Restore(cmd.Context(), projectID, offeringID)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Restored %s", o.ID))
			return rt.Out.Render(o)
		},
	}
}

func newOfferingsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List offerings",
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
			page, err := client.Offerings.List(cmd.Context(), projectID)
			if err != nil {
				return err
			}

			if !rt.Globals.JSON && !rt.Globals.NoInput && tui.IsInteractive() {
				items := make([]tui.BrowserItem, len(page.Items))
				for i, o := range page.Items {
					o := o
					items[i] = offeringToItem(cmd.Context(), client, projectID, o)
				}
				return tui.RunBrowserTable("Offerings", []string{"ID", "LOOKUP KEY", "DISPLAY NAME", "STATE"}, items)
			}

			rows := make([][]string, 0, len(page.Items))
			for _, o := range page.Items {
				current := " "
				if o.IsCurrent {
					current = "*"
				}
				rows = append(rows, []string{current, o.ID, o.LookupKey, o.DisplayName, string(o.State), formatMillis(o.CreatedAt)})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"", "ID", "LOOKUP KEY", "DISPLAY NAME", "STATE", "CREATED"},
				Rows:    rows,
				Raw:     page,
			})
		},
	}
}

func newOfferingsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [id]",
		Short: "Show an offering",
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
			offeringID, err := requireID(rt, argAt(args, 0), "offering", func() ([]PickerItem, error) {
				return offeringPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			o, err := client.Offerings.Get(cmd.Context(), projectID, offeringID)
			if err != nil {
				return err
			}
			if !rt.Globals.JSON && !rt.Globals.NoInput && tui.IsInteractive() {
				item := offeringToItem(cmd.Context(), client, projectID, *o)
				return tui.RunBrowser("Offering", []tui.BrowserItem{item})
			}
			return rt.Out.Render(o)
		},
	}
}

func newOfferingsCreateCmd() *cobra.Command {
	var lookupKey, displayName string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an offering",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if err := tui.Form(rt.Globals.NoInput).
				Field(huh.NewInput().Title("Lookup key").Value(&lookupKey).Validate(tui.Required("lookup key"))).
				Field(huh.NewInput().Title("Display name").Value(&displayName).Validate(tui.Required("display name"))).
				Run(); err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			o, err := client.Offerings.Create(cmd.Context(), projectID, api.OfferingCreate{
				LookupKey: lookupKey, DisplayName: displayName,
			})
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Created offering %s", o.ID))
			return rt.Out.Render(o)
		},
	}
	cmd.Flags().StringVar(&lookupKey, "lookup-key", "", "lookup key (required)")
	cmd.Flags().StringVar(&displayName, "display-name", "", "display name (required)")
	return cmd
}

func newOfferingsUpdateCmd() *cobra.Command {
	var displayName string
	cmd := &cobra.Command{
		Use:   "update [id]",
		Short: "Update an offering",
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
			offeringID, err := requireID(rt, argAt(args, 0), "offering", func() ([]PickerItem, error) {
				return offeringPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			body := api.OfferingUpdate{}
			if cmd.Flags().Changed("display-name") {
				body.DisplayName = &displayName
			}
			o, err := client.Offerings.Update(cmd.Context(), projectID, offeringID, body)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Updated %s", o.ID))
			return rt.Out.Render(o)
		},
	}
	cmd.Flags().StringVar(&displayName, "display-name", "", "new display name")
	return cmd
}

func newOfferingsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete [id]",
		Short: "Delete an offering",
		Long: `Permanently deletes an offering from the project.

Reversibility: irreversible. Prefer ` + "`rc offerings archive`" + ` for
reversible removal.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc offerings delete ofrng_old --yes`,
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
			offeringID, err := requireID(rt, argAt(args, 0), "offering", func() ([]PickerItem, error) {
				return offeringPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			if err := confirmOrAbort(rt, fmt.Sprintf("Delete offering %q?", offeringID)); err != nil {
				return err
			}
			if err := client.Offerings.Delete(cmd.Context(), projectID, offeringID); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Deleted %s", offeringID))
			return rt.Out.Render(map[string]any{"ok": true, "id": offeringID})
		},
	}
}

// ── browser helpers ──────────────────────────────────────────────────────────

func offeringToItem(ctx context.Context, client *api.Client, projectID string, o api.Offering) tui.BrowserItem {
	state := string(o.State)
	meta := state
	if o.IsCurrent {
		meta = "current · " + meta
	}
	offeringURL := fmt.Sprintf("https://app.revenuecat.com/projects/%s/offerings/%s", dashboardProjectID(projectID), o.ID)
	return tui.BrowserItem{
		ID:     o.ID,
		Label:  o.LookupKey,
		Meta:   meta,
		Row:    []string{o.ID, o.LookupKey, o.DisplayName, state},
		WebURL: offeringURL,
		Fields: []tui.BrowserField{
			{Key: "ID", Value: o.ID},
			{Key: "Lookup key", Value: o.LookupKey},
			{Key: "Display name", Value: o.DisplayName},
			{Key: "State", Value: state},
			{Key: "Current", Value: fmt.Sprintf("%v", o.IsCurrent)},
			{Key: "Created", Value: formatMillis(o.CreatedAt)},
		},
		AutoLoad: func() ([]tui.BrowserSection, error) {
			var sections []tui.BrowserSection

			// Packages — selectable, drills to package detail
			pp, err := client.Packages.List(ctx, projectID, o.ID)
			if err != nil {
				return nil, err
			}
			{
				sec := tui.BrowserSection{
					Title: "Packages",
					Cols:  []string{"LOOKUP KEY", "DISPLAY NAME", "POSITION"},
					Empty: "no packages",
				}
				for _, p := range pp.Items {
					p := p
					item := packageToItem(ctx, client, projectID, o.ID, p)
					pos := ""
					if p.Position != nil {
						pos = fmt.Sprintf("%d", *p.Position)
					}
					sec.Rows = append(sec.Rows, tui.BrowserSectionRow{
						Cells: []string{p.LookupKey, p.DisplayName, pos},
						Item:  &item,
					})
				}
				sections = append(sections, sec)
			}

			// Paywalls — selectable, drills to paywall detail (leaf)
			pws, err := client.Paywalls.List(ctx, projectID)
			if err == nil {
				sec := tui.BrowserSection{
					Title: "Paywalls",
					Cols:  []string{"NAME", "PUBLISHED"},
					Empty: "no paywalls",
				}
				for _, pw := range pws.Items {
					if pw.OfferingID != o.ID {
						continue
					}
					pw := pw
					item := paywallToItem(projectID, pw)
					sec.Rows = append(sec.Rows, tui.BrowserSectionRow{
						Cells: []string{pw.Name, formatPublishedAt(pw.PublishedAt)},
						Item:  &item,
					})
				}
				sections = append(sections, sec)
			}

			return sections, nil
		},
	}
}

// packageToItem builds a detail item for a package, with its products loaded via AutoLoad.
func packageToItem(ctx context.Context, client *api.Client, projectID, offeringID string, p api.Package) tui.BrowserItem {
	offeringURL := fmt.Sprintf("https://app.revenuecat.com/projects/%s/offerings/%s", dashboardProjectID(projectID), offeringID)
	return tui.BrowserItem{
		ID:     p.ID,
		Label:  p.LookupKey,
		Meta:   p.DisplayName,
		WebURL: offeringURL,
		Fields: []tui.BrowserField{
			{Key: "ID", Value: p.ID},
			{Key: "Lookup key", Value: p.LookupKey},
			{Key: "Display name", Value: p.DisplayName},
			{Key: "Created", Value: formatMillis(p.CreatedAt)},
		},
		AutoLoad: func() ([]tui.BrowserSection, error) {
			prods, err := client.Packages.ListProducts(ctx, projectID, p.ID)
			if err != nil {
				return nil, err
			}
			sec := tui.BrowserSection{
				Title: "Products",
				Cols:  []string{"DISPLAY NAME", "STORE ID", "TYPE", "STATE"},
				Empty: "no products attached",
			}
			for _, association := range prods.Items {
				prod := association.Product
				item := productToItem(prod)
				sec.Rows = append(sec.Rows, tui.BrowserSectionRow{
					Cells: []string{derefStr(prod.DisplayName), prod.StoreIdentifier, string(prod.Type), string(prod.State)},
					Item:  &item,
				})
			}
			return []tui.BrowserSection{sec}, nil
		},
	}
}

// paywallToItem builds a leaf detail item for a paywall.
func paywallToItem(projectID string, pw api.Paywall) tui.BrowserItem {
	scaleFonts := "no"
	if pw.AutomaticallyScaleFontSize {
		scaleFonts = "yes"
	}
	return tui.BrowserItem{
		ID:     pw.ID,
		Label:  pw.Name,
		Meta:   formatPublishedAt(pw.PublishedAt),
		WebURL: fmt.Sprintf("https://app.revenuecat.com/projects/%s/offerings", dashboardProjectID(projectID)),
		Fields: []tui.BrowserField{
			{Key: "ID", Value: pw.ID},
			{Key: "Name", Value: pw.Name},
			{Key: "Auto-scale fonts", Value: scaleFonts},
			{Key: "Created", Value: formatMillis(int64(pw.CreatedAt))},
			{Key: "Published", Value: formatPublishedAt(pw.PublishedAt)},
		},
	}
}

func newOfferingsPackagesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "packages [offering-id]",
		Short: "List packages in an offering",
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
			offeringID, err := requireID(rt, argAt(args, 0), "offering", func() ([]PickerItem, error) {
				return offeringPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			page, err := client.Packages.List(cmd.Context(), projectID, offeringID)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(page.Items))
			for _, p := range page.Items {
				rows = append(rows, []string{p.ID, p.LookupKey, p.DisplayName, formatMillis(p.CreatedAt)})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "LOOKUP KEY", "DISPLAY NAME", "CREATED"},
				Rows:    rows,
				Raw:     page,
			})
		},
	}
}

// ── picker helpers ───────────────────────────────────────────────────────────

func offeringPickerItems(ctx context.Context, client *api.Client, projectID string) ([]PickerItem, error) {
	page, err := client.Offerings.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	items := make([]PickerItem, len(page.Items))
	for i, o := range page.Items {
		label := o.LookupKey
		if o.DisplayName != "" {
			label = fmt.Sprintf("%s  (%s)", o.DisplayName, o.LookupKey)
		}
		if o.IsCurrent {
			label = "* " + label
		}
		items[i] = PickerItem{ID: o.ID, Label: label}
	}
	return items, nil
}
