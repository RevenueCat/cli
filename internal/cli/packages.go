package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

// Package creation is scoped to an offering. Existing package operations use
// package IDs directly, matching the public v2 package endpoints.

func newPackagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "packages",
		Aliases: []string{"package", "pkg"},
		Short:   "Manage Packages within Offerings",
		Long: `A Package groups equivalent Products across platforms under one identifier
(such as $rc_monthly or $rc_annual) so a paywall can offer the right Product on
each store. Packages live inside an Offering. Run bare to list every Package in
the project.`,
		Example: `  rc packages list
  rc packages create ofrng_default --lookup-key '$rc_monthly' --display-name "Monthly"`,
		// Bare `rc packages` runs list for discovery.
		RunE: runPackagesList,
	}
	cmd.AddCommand(
		newPackagesListCmd(),
		newPackagesShowCmd(),
		newPackagesCreateCmd(),
		newPackagesUpdateCmd(),
		newPackagesDeleteCmd(),
		newPackagesProductsCmd(),
		newPackagesAttachCmd(),
		newPackagesDetachCmd(),
	)
	return cmd
}

func newPackagesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all Packages across all Offerings",
		Long: `Lists every Package in the active project, grouped across all Offerings.
Each row includes the Offering ID so you know where the Package lives.

To create, update, or delete a Package you need both the Offering ID and
the Package ID. This command is the fastest way to discover them.`,
		Example: `  rc packages list
  rc packages list --json | jq '.data.items[] | select(.lookup_key == "$rc_monthly")'`,
		RunE: runPackagesList,
	}
}

func runPackagesList(cmd *cobra.Command, _ []string) error {
	rt := RuntimeFrom(cmd.Context())
	projectID, err := requireProject(rt)
	if err != nil {
		return err
	}
	client, err := rt.API()
	if err != nil {
		return err
	}
	offerings, err := client.Offerings.List(cmd.Context(), projectID)
	if err != nil {
		return err
	}
	type flatPkg struct {
		offeringID  string
		offeringKey string
		pkg         api.Package
	}
	var all []flatPkg
	for _, o := range offerings.Items {
		pkgs, err := client.Packages.List(cmd.Context(), projectID, o.ID)
		if err != nil {
			return fmt.Errorf("listing packages for offering %s: %w", o.ID, err)
		}
		key := o.LookupKey
		if key == "" {
			key = o.ID
		}
		for _, p := range pkgs.Items {
			all = append(all, flatPkg{o.ID, key, p})
		}
	}

	if !rt.Globals.JSON && !rt.Globals.NoInput && tui.IsInteractive() {
		items := make([]tui.BrowserItem, len(all))
		for i, fp := range all {
			fp := fp
			label := fp.pkg.LookupKey
			if fp.pkg.DisplayName != "" {
				label = fp.pkg.DisplayName
			}
			items[i] = tui.BrowserItem{
				ID:    fp.pkg.ID,
				Label: label,
				Meta:  fp.offeringKey,
				Row:   []string{fp.pkg.ID, fp.pkg.LookupKey, fp.pkg.DisplayName, fp.offeringID},
				Fields: []tui.BrowserField{
					{Key: "ID", Value: fp.pkg.ID},
					{Key: "Lookup key", Value: fp.pkg.LookupKey},
					{Key: "Display name", Value: fp.pkg.DisplayName},
					{Key: "Offering ID", Value: fp.offeringID},
					{Key: "Offering key", Value: fp.offeringKey},
					{Key: "Created", Value: formatMillis(fp.pkg.CreatedAt)},
				},
			}
		}
		return tui.RunBrowserTable("Packages", []string{"ID", "LOOKUP KEY", "DISPLAY NAME", "OFFERING"}, items)
	}

	rawItems := make([]map[string]any, len(all))
	rows := make([][]string, len(all))
	for i, fp := range all {
		rows[i] = []string{fp.pkg.ID, fp.pkg.LookupKey, fp.pkg.DisplayName, fp.offeringID}
		rawItems[i] = map[string]any{
			"id":           fp.pkg.ID,
			"lookup_key":   fp.pkg.LookupKey,
			"display_name": fp.pkg.DisplayName,
			"offering_id":  fp.offeringID,
			"offering_key": fp.offeringKey,
		}
	}
	return rt.Out.RenderTable(output.Table{
		Columns: []string{"ID", "LOOKUP KEY", "DISPLAY NAME", "OFFERING"},
		Rows:    rows,
		Raw:     map[string]any{"items": rawItems},
	})
}

func newPackagesShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [package-id]",
		Short: "Show a Package",
		Long: `Shows a Package's lookup key and display name. Omit the ID in a terminal
to pick from a list.`,
		Example: `  rc packages show pkg_x
  rc packages show          # pick interactively`,
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
			packageID, err := requireID(rt, argAt(args, 0), "package", func() ([]PickerItem, error) {
				return allPackagePickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			p, err := client.Packages.Get(cmd.Context(), projectID, packageID)
			if err != nil {
				return err
			}
			return rt.Out.Render(p)
		},
	}
}

func newPackagesCreateCmd() *cobra.Command {
	var lookupKey, displayName string
	cmd := &cobra.Command{
		Use:   "create [offering-id]",
		Short: "Create a Package in an Offering",
		Long: `Creates a Package inside an Offering. Use a standard lookup key such as
$rc_monthly or $rc_annual so SDKs and paywalls resolve it automatically. Omit
the Offering ID in a terminal to pick from a list. Attach Products afterward
with ` + "`rc packages attach`" + `.`,
		Example: `  rc packages create ofrng_default --lookup-key '$rc_monthly' --display-name "Monthly"`,
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
			if lookupKey == "" {
				return fmt.Errorf("--lookup-key is required")
			}
			if displayName == "" {
				return fmt.Errorf("--display-name is required")
			}
			pkg, err := client.Packages.Create(cmd.Context(), projectID, offeringID, api.PackageCreate{
				LookupKey: lookupKey, DisplayName: displayName,
			})
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Created package %s", pkg.ID))
			return rt.Out.Render(pkg)
		},
	}
	cmd.Flags().StringVar(&lookupKey, "lookup-key", "", "lookup key (required)")
	cmd.Flags().StringVar(&displayName, "display-name", "", "display name (required)")
	return cmd
}

func newPackagesUpdateCmd() *cobra.Command {
	var displayName string
	cmd := &cobra.Command{
		Use:     "update [package-id]",
		Short:   "Update a Package",
		Long:    `Updates a Package's display name. Omit the ID in a terminal to pick from a list.`,
		Example: `  rc packages update pkg_x --display-name "Monthly"`,
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
			packageID, err := requireID(rt, argAt(args, 0), "package", func() ([]PickerItem, error) {
				return allPackagePickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			body := api.PackageUpdate{}
			if cmd.Flags().Changed("display-name") {
				body.DisplayName = &displayName
			}
			pkg, err := client.Packages.Update(cmd.Context(), projectID, packageID, body)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Updated %s", pkg.ID))
			return rt.Out.Render(pkg)
		},
	}
	cmd.Flags().StringVar(&displayName, "display-name", "", "new display name")
	return cmd
}

func newPackagesDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete [package-id]",
		Short: "Delete a Package",
		Long: `Permanently deletes a Package from its Offering. Omit the ID in a terminal
to pick from a list.

Reversibility: irreversible.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc packages delete pkg_x --yes`,
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
			packageID, err := requireID(rt, argAt(args, 0), "package", func() ([]PickerItem, error) {
				return allPackagePickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			if err := confirmOrAbort(rt, fmt.Sprintf("Delete package %q?", packageID)); err != nil {
				return err
			}
			if err := client.Packages.Delete(cmd.Context(), projectID, packageID); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Deleted %s", packageID))
			return rt.Out.Render(map[string]any{"ok": true, "id": packageID})
		},
	}
}

func newPackagesProductsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "products [package-id]",
		Short: "List Products attached to a Package",
		Long: `Lists the Products attached to a Package, with each store identifier and
eligibility criteria. Omit the ID in a terminal to pick from a list.`,
		Example: `  rc packages products pkg_x
  rc packages products pkg_x --json | jq '.data.items[].store_identifier'`,
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
			packageID, err := requireID(rt, argAt(args, 0), "package", func() ([]PickerItem, error) {
				return allPackagePickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			page, err := client.Packages.ListProducts(cmd.Context(), projectID, packageID)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(page.Items))
			for _, association := range page.Items {
				p := association.Product
				rows = append(rows, []string{p.ID, derefStr(p.DisplayName), p.StoreIdentifier, string(association.EligibilityCriteria)})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "DISPLAY NAME", "STORE ID", "ELIGIBILITY"},
				Rows:    rows,
				Raw:     page,
			})
		},
	}
}

// ── picker helpers ───────────────────────────────────────────────────────────

func allPackagePickerItems(ctx context.Context, client *api.Client, projectID string) ([]PickerItem, error) {
	offerings, err := client.Offerings.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var items []PickerItem
	for _, o := range offerings.Items {
		pkgs, err := client.Packages.List(ctx, projectID, o.ID)
		if err != nil {
			return nil, fmt.Errorf("listing packages for offering %s: %w", o.ID, err)
		}
		offeringLabel := o.LookupKey
		if offeringLabel == "" {
			offeringLabel = o.ID
		}
		for _, p := range pkgs.Items {
			label := p.LookupKey
			if p.DisplayName != "" {
				label = fmt.Sprintf("%s  (%s)", p.DisplayName, p.LookupKey)
			}
			items = append(items, PickerItem{ID: p.ID, Label: fmt.Sprintf("%s  ·  %s", label, offeringLabel)})
		}
	}
	return items, nil
}

func newPackagesAttachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attach <package-id> <product-id> [product-id...]",
		Short: "Attach Products to a Package",
		Long: `Attaches one or more Products to a Package. Positional arguments are the
Package ID followed by every Product ID to attach. Products default to the
"all" eligibility criteria, which applies to every supported SDK version.`,
		Example: `  rc packages attach pkg_x prod_monthly
  rc packages attach pkg_x prod_ios_monthly prod_android_monthly --json --no-input`,
		Args: cobra.MinimumNArgs(2),
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
			if err := client.Packages.AttachProducts(cmd.Context(), projectID, args[0], args[1:]); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Attached %d product(s)", len(args)-1))
			return rt.Out.Render(map[string]any{"ok": true, "package_id": args[0], "product_ids": args[1:]})
		},
	}
}

func newPackagesDetachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "detach <package-id> <product-id> [product-id...]",
		Short: "Detach Products from a Package",
		Long: `Detaches one or more Products from a Package. Positional arguments are the
Package ID followed by every Product ID to detach.`,
		Example: `  rc packages detach pkg_x prod_legacy --json --no-input`,
		Args:    cobra.MinimumNArgs(2),
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
			if err := client.Packages.DetachProducts(cmd.Context(), projectID, args[0], args[1:]); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Detached %d product(s)", len(args)-1))
			return rt.Out.Render(map[string]any{"ok": true, "package_id": args[0], "product_ids": args[1:]})
		},
	}
}
