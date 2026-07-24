package cli

import (
	"context"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

// Entitlements is the first catalog-CRUD resource. The shape here is the
// template for offerings/products/packages: list, show, create, update, delete.
// If a third resource ends up identical, extract a generic helper — not before.

func newEntitlementsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "entitlements",
		Aliases: []string{"entitlement", "ent"},
		Short:   "Manage entitlements in the project catalog",
	}
	cmd.AddCommand(
		newEntitlementsListCmd(),
		newEntitlementsShowCmd(),
		newEntitlementsCreateCmd(),
		newEntitlementsUpdateCmd(),
		newEntitlementsDeleteCmd(),
		newEntitlementsArchiveCmd(),
		newEntitlementsRestoreCmd(),
		newEntitlementsProductsCmd(),
		newEntitlementsAttachCmd(),
		newEntitlementsDetachCmd(),
	)
	return cmd
}

func newEntitlementsArchiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "archive [id]",
		Short: "Archive an entitlement",
		Long: `Archives an entitlement, removing it from new offerings while preserving
historical access for existing subscribers.

Reversibility: use ` + "`rc entitlements restore <id>`" + ` to undo.

Confirmation: no prompt — this is a soft, reversible state change.`,
		Example: `  rc entitlements archive entl_legacy`,
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
			entID, err := requireID(rt, argAt(args, 0), "entitlement", func() ([]PickerItem, error) {
				return entitlementPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			e, err := client.Entitlements.Archive(cmd.Context(), projectID, entID)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Archived %s", e.ID))
			return rt.Out.Render(e)
		},
	}
}

func newEntitlementsRestoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore [id]",
		Short: "Restore an archived entitlement (= unarchive)",
		Long: `Restores a previously-archived entitlement so it can be added to new
offerings again. Inverse of ` + "`rc entitlements archive`" + `.

Reversibility: re-archive with ` + "`rc entitlements archive <id>`" + `.

Confirmation: no prompt.`,
		Example: `  rc entitlements restore entl_legacy`,
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
			entID, err := requireID(rt, argAt(args, 0), "entitlement", func() ([]PickerItem, error) {
				return entitlementPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			e, err := client.Entitlements.Restore(cmd.Context(), projectID, entID)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Restored %s", e.ID))
			return rt.Out.Render(e)
		},
	}
}

func newEntitlementsProductsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "products [id]",
		Short: "List products attached to an entitlement",
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
			entID, err := requireID(rt, argAt(args, 0), "entitlement", func() ([]PickerItem, error) {
				return entitlementPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			page, err := client.Entitlements.ListProducts(cmd.Context(), projectID, entID)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(page.Items))
			for _, p := range page.Items {
				rows = append(rows, []string{p.ID, p.DisplayName, p.StoreIdentifier, p.Type})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "DISPLAY NAME", "STORE ID", "TYPE"},
				Rows:    rows,
				Raw:     page,
			})
		},
	}
}

func newEntitlementsAttachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attach <id> <product-id> [product-id...]",
		Short: "Attach products to an entitlement",
		Long: `Attaches one or more products to an entitlement. The entitlement will
then grant access to anyone who purchases any of the listed products.`,
		Example: `  rc entitlements attach pro prod_monthly prod_yearly
  rc entitlements attach pro prod_monthly --json`,
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
			if err := client.Entitlements.AttachProducts(cmd.Context(), projectID, args[0], args[1:]); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Attached %d product(s) to %s", len(args)-1, args[0]))
			return rt.Out.Render(map[string]any{"ok": true, "entitlement_id": args[0], "product_ids": args[1:]})
		},
	}
}

func newEntitlementsDetachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "detach <id> <product-id> [product-id...]",
		Short: "Detach products from an entitlement",
		Args:  cobra.MinimumNArgs(2),
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
			if err := client.Entitlements.DetachProducts(cmd.Context(), projectID, args[0], args[1:]); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Detached %d product(s) from %s", len(args)-1, args[0]))
			return rt.Out.Render(map[string]any{"ok": true, "entitlement_id": args[0], "product_ids": args[1:]})
		},
	}
}

func newEntitlementsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List entitlements",
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
			page, err := client.Entitlements.List(cmd.Context(), projectID)
			if err != nil {
				return err
			}

			if !rt.Globals.JSON && !rt.Globals.NoInput && tui.IsInteractive() {
				items := make([]tui.BrowserItem, len(page.Items))
				for i, e := range page.Items {
					e := e
					items[i] = entitlementToItem(cmd.Context(), client, projectID, e)
				}
				return tui.RunBrowserTable("Entitlements", []string{"ID", "LOOKUP KEY", "DISPLAY NAME"}, items)
			}

			rows := make([][]string, 0, len(page.Items))
			for _, e := range page.Items {
				rows = append(rows, []string{e.ID, e.LookupKey, e.DisplayName, formatMillis(e.CreatedAt)})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "LOOKUP KEY", "DISPLAY NAME", "CREATED"},
				Rows:    rows,
				Raw:     page,
			})
		},
	}
}

func newEntitlementsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [id]",
		Short: "Show an entitlement",
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
			entID, err := requireID(rt, argAt(args, 0), "entitlement", func() ([]PickerItem, error) {
				return entitlementPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			ent, err := client.Entitlements.Get(cmd.Context(), projectID, entID)
			if err != nil {
				return err
			}
			if !rt.Globals.JSON && !rt.Globals.NoInput && tui.IsInteractive() {
				item := entitlementToItem(cmd.Context(), client, projectID, *ent)
				return tui.RunBrowser("Entitlement", []tui.BrowserItem{item})
			}
			return rt.Out.Render(ent)
		},
	}
}

func newEntitlementsCreateCmd() *cobra.Command {
	var lookupKey, displayName string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an entitlement",
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
			ent, err := client.Entitlements.Create(cmd.Context(), projectID, api.EntitlementCreate{
				LookupKey:   lookupKey,
				DisplayName: displayName,
			})
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Created entitlement %s", ent.ID))
			return rt.Out.Render(ent)
		},
	}
	cmd.Flags().StringVar(&lookupKey, "lookup-key", "", "lookup key (required)")
	cmd.Flags().StringVar(&displayName, "display-name", "", "display name (required)")
	return cmd
}

func newEntitlementsUpdateCmd() *cobra.Command {
	var displayName string
	cmd := &cobra.Command{
		Use:   "update [id]",
		Short: "Update an entitlement",
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
			entID, err := requireID(rt, argAt(args, 0), "entitlement", func() ([]PickerItem, error) {
				return entitlementPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			body := api.EntitlementUpdate{}
			if cmd.Flags().Changed("display-name") {
				body.DisplayName = &displayName
			}
			ent, err := client.Entitlements.Update(cmd.Context(), projectID, entID, body)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Updated %s", ent.ID))
			return rt.Out.Render(ent)
		},
	}
	cmd.Flags().StringVar(&displayName, "display-name", "", "new display name")
	return cmd
}

func newEntitlementsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete [id]",
		Short: "Delete an entitlement",
		Long: `Permanently deletes an entitlement from the project catalog.

Reversibility: irreversible. If you only need to hide it from current
offerings, prefer ` + "`rc entitlements archive`" + ` which can be undone with
` + "`rc entitlements restore`" + `.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc entitlements delete entl_old --yes`,
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
			entID, err := requireID(rt, argAt(args, 0), "entitlement", func() ([]PickerItem, error) {
				return entitlementPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			if !rt.Globals.AssumeYes {
				ok, err := tui.Confirm(rt.Globals.NoInput, fmt.Sprintf("Delete entitlement %q?", entID))
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("aborted")
				}
			}
			if err := client.Entitlements.Delete(cmd.Context(), projectID, entID); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Deleted %s", entID))
			return rt.Out.Render(map[string]any{"ok": true, "id": entID})
		},
	}
}

// ── picker helpers ───────────────────────────────────────────────────────────

func entitlementPickerItems(ctx context.Context, client *api.Client, projectID string) ([]PickerItem, error) {
	page, err := client.Entitlements.List(ctx, projectID)
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
}

// ── browser helpers ──────────────────────────────────────────────────────────

// entitlementToItem works for both catalog entitlements and active/contextual
// entitlements (Source/Granted/Expires are shown when non-zero).
func entitlementToItem(ctx context.Context, client *api.Client, projectID string, e api.Entitlement) tui.BrowserItem {
	label := e.LookupKey
	if label == "" {
		label = e.ID
	}
	return tui.BrowserItem{
		ID:     e.ID,
		Label:  label,
		Meta:   e.DisplayName,
		Row:    []string{e.ID, nonEmpty(e.LookupKey, e.ID), e.DisplayName},
		WebURL: fmt.Sprintf("https://app.revenuecat.com/projects/%s/entitlements/%s", dashboardProjectID(projectID), e.ID),
		Fields: []tui.BrowserField{
			{Key: "ID", Value: e.ID},
			{Key: "Lookup key", Value: e.LookupKey},
			{Key: "Display name", Value: e.DisplayName},
			{Key: "Source", Value: e.Source},
			{Key: "Granted", Value: formatMillis(e.GrantedAt)},
			{Key: "Expires", Value: formatMillis(e.ExpiresAt)},
			{Key: "Created", Value: formatMillis(e.CreatedAt)},
		},
		AutoLoad: func() ([]tui.BrowserSection, error) {
			pp, err := client.Entitlements.ListProducts(ctx, projectID, e.ID)
			if err != nil {
				return nil, err
			}
			sec := tui.BrowserSection{
				Title: "Products",
				Cols:  []string{"DISPLAY NAME", "STORE ID", "TYPE", "STATE"},
				Empty: "no products attached",
			}
			for _, p := range pp.Items {
				p := p
				item := productToItem(p)
				sec.Rows = append(sec.Rows, tui.BrowserSectionRow{
					Cells: []string{p.DisplayName, p.StoreIdentifier, p.Type, p.State},
					Item:  &item,
				})
			}
			return []tui.BrowserSection{sec}, nil
		},
	}
}
