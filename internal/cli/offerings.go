package cli

import (
	"fmt"

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
		newOfferingsCreateCmd(),
		newOfferingsUpdateCmd(),
		newOfferingsDeleteCmd(),
		newOfferingsArchiveCmd(),
		newOfferingsRestoreCmd(),
		newOfferingsPackagesCmd(),
	)
	return cmd
}

func newOfferingsArchiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "archive <id>",
		Short: "Archive an offering",
		Long: `Archives an offering so it stops being served to new customers while
existing subscribers keep their access.

Reversibility: use ` + "`rc offerings restore <id>`" + ` to undo.

Confirmation: no prompt — soft, reversible state change.`,
		Example: `  rc offerings archive ofrng_q1_promo`,
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
			o, err := client.Offerings.Archive(cmd.Context(), projectID, args[0])
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
		Use:   "restore <id>",
		Short: "Restore an archived offering (= unarchive)",
		Long: `Restores a previously-archived offering. Inverse of
` + "`rc offerings archive`" + `.

Reversibility: re-archive with ` + "`rc offerings archive <id>`" + `.

Confirmation: no prompt.`,
		Example: `  rc offerings restore ofrng_q1_promo`,
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
			o, err := client.Offerings.Restore(cmd.Context(), projectID, args[0])
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
			rows := make([][]string, 0, len(page.Items))
			for _, o := range page.Items {
				current := " "
				if o.IsCurrent {
					current = "*"
				}
				rows = append(rows, []string{current, o.ID, o.LookupKey, o.DisplayName, o.State, formatMillis(o.CreatedAt)})
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
		Use:   "show <id>",
		Short: "Show an offering",
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
			o, err := client.Offerings.Get(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
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
				Field(huh.NewInput().Title("Display name (optional)").Value(&displayName)).
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
	cmd.Flags().StringVar(&displayName, "display-name", "", "display name")
	return cmd
}

func newOfferingsUpdateCmd() *cobra.Command {
	var displayName string
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update an offering",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			body := api.OfferingUpdate{}
			if cmd.Flags().Changed("display-name") {
				body.DisplayName = &displayName
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			o, err := client.Offerings.Update(cmd.Context(), projectID, args[0], body)
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
		Use:   "delete <id>",
		Short: "Delete an offering",
		Long: `Permanently deletes an offering from the project.

Reversibility: irreversible. Prefer ` + "`rc offerings archive`" + ` for
reversible removal.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc offerings delete ofrng_old --yes`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if !rt.Globals.AssumeYes {
				ok, err := tui.Confirm(rt.Globals.NoInput, fmt.Sprintf("Delete offering %q?", args[0]))
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
			if err := client.Offerings.Delete(cmd.Context(), projectID, args[0]); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Deleted %s", args[0]))
			return rt.Out.Render(map[string]any{"ok": true, "id": args[0]})
		},
	}
}

func newOfferingsPackagesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "packages <offering-id>",
		Short: "List packages in an offering",
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
			page, err := client.Packages.List(cmd.Context(), projectID, args[0])
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
