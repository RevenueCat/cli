package cli

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

// Packages live inside offerings: every command takes <offering-id> as the
// first arg. `rc offerings packages <id>` covers list-by-offering; this group
// covers the per-package CRUD + product attachment.

func newPackagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "packages",
		Aliases: []string{"package", "pkg"},
		Short:   "Manage packages within offerings",
	}
	cmd.AddCommand(
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

func newPackagesShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <offering-id> <package-id>",
		Short: "Show a package",
		Args:  cobra.ExactArgs(2),
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
			p, err := client.Packages.Get(cmd.Context(), projectID, args[0], args[1])
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
		Use:   "create <offering-id>",
		Short: "Create a package in an offering",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if err := tui.Form(rt.Globals.NoInput).
				Field(huh.NewInput().Title("Lookup key").Value(&lookupKey).Validate(tui.Required("lookup key"))).
				Field(huh.NewInput().Title("Display name (optional)").Value(&displayName)).
				Run(); err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			pkg, err := client.Packages.Create(cmd.Context(), projectID, args[0], api.PackageCreate{
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
		Use:   "update <offering-id> <package-id>",
		Short: "Update a package",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			body := api.PackageUpdate{}
			if cmd.Flags().Changed("display-name") {
				body.DisplayName = &displayName
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			pkg, err := client.Packages.Update(cmd.Context(), projectID, args[0], args[1], body)
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
		Use:   "delete <offering-id> <package-id>",
		Short: "Delete a package",
		Long: `Permanently deletes a package from its offering.

Reversibility: irreversible.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc packages delete ofrng_default pkg_legacy --yes`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if !rt.Globals.AssumeYes {
				ok, err := tui.Confirm(rt.Globals.NoInput, fmt.Sprintf("Delete package %q?", args[1]))
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
			if err := client.Packages.Delete(cmd.Context(), projectID, args[0], args[1]); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Deleted %s", args[1]))
			return rt.Out.Render(map[string]any{"ok": true, "id": args[1]})
		},
	}
}

func newPackagesProductsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "products <offering-id> <package-id>",
		Short: "List products attached to a package",
		Args:  cobra.ExactArgs(2),
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
			page, err := client.Packages.ListProducts(cmd.Context(), projectID, args[0], args[1])
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(page.Items))
			for _, p := range page.Items {
				rows = append(rows, []string{p.ID, p.DisplayName, p.StoreIdentifier})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "DISPLAY NAME", "STORE ID"},
				Rows:    rows,
				Raw:     page,
			})
		},
	}
}

func newPackagesAttachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attach <offering-id> <package-id> <product-id> [product-id...]",
		Short: "Attach products to a package",
		Args:  cobra.MinimumNArgs(3),
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
			if err := client.Packages.AttachProducts(cmd.Context(), projectID, args[0], args[1], args[2:]); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Attached %d product(s)", len(args)-2))
			return rt.Out.Render(map[string]any{"ok": true, "package_id": args[1], "product_ids": args[2:]})
		},
	}
}

func newPackagesDetachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "detach <offering-id> <package-id> <product-id> [product-id...]",
		Short: "Detach products from a package",
		Args:  cobra.MinimumNArgs(3),
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
			if err := client.Packages.DetachProducts(cmd.Context(), projectID, args[0], args[1], args[2:]); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Detached %d product(s)", len(args)-2))
			return rt.Out.Render(map[string]any{"ok": true, "package_id": args[1], "product_ids": args[2:]})
		},
	}
}
