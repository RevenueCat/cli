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
		Short:   "Manage packages within offerings",
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
		Short: "List all packages across all offerings",
		Long: `Lists every package in the active project, grouped across all offerings.
Each row includes the offering ID so you know where to find the package.

To create, update, or delete a package you need both the offering ID and
the package ID. This command is the fastest way to discover them.`,
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
		Short: "Show a package",
		Long: `Show details for a specific package. Omit the ID under a TTY to pick
interactively.`,
		Example: `  rc packages show          # TTY: pick a package
  rc packages show pkg_xyz  # explicit`,
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
		Short: "Create a package in an offering",
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
			if lookupKey == "" {
				return fmt.Errorf("--lookup-key is required")
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
	cmd.Flags().StringVar(&displayName, "display-name", "", "display name")
	return cmd
}

func newPackagesUpdateCmd() *cobra.Command {
	var displayName string
	cmd := &cobra.Command{
		Use:   "update [package-id]",
		Short: "Update a package",
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
		Short: "Delete a package",
		Long: `Permanently deletes a package from its offering.

Reversibility: irreversible.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc packages delete pkg_legacy --yes`,
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
			if !rt.Globals.AssumeYes {
				ok, err := tui.Confirm(rt.Globals.NoInput, fmt.Sprintf("Delete package %q?", packageID))
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("aborted")
				}
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
		Short: "List products attached to a package",
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
		Short: "Attach products to a package",
		Long: `Attach one or more products to a package. Positional arguments are the
package ID followed by every product ID to attach. Products default to the
"all" eligibility criteria, which applies to every supported SDK version.`,
		Example: `  rc packages attach pkg_monthly prod_test_monthly
  rc packages attach pkg_monthly prod_ios_monthly prod_android_monthly --json --no-input`,
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
		Short: "Detach products from a package",
		Long: `Detach one or more products from a package. Positional arguments are the
package ID followed by every product ID to detach.`,
		Example: `  rc packages detach pkg_monthly prod_legacy --json --no-input`,
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
