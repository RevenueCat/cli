package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
	"github.com/revenuecat/cli/internal/tui"
)

func newPaywallsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "paywalls",
		Aliases: []string{"paywall"},
		Short:   "Create and inspect paywalls",
	}
	cmd.AddCommand(
		newPaywallsListCmd(),
		newPaywallsShowCmd(),
		newPaywallsCreateCmd(),
		newPaywallsPublishCmd(),
		newPaywallsDeleteCmd(),
	)
	return cmd
}

func newPaywallsCreateCmd() *cobra.Command {
	var offeringID string
	var automaticallyScaleFontSize bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a draft paywall for an offering",
		Long: `Creates a draft paywall using RevenueCat's default template and attaches it
to an offering. The offering must already contain at least one package.

After creation, review the draft and run ` + "`rc paywalls publish <id>`" + ` to make
it available to RevenueCat SDKs.`,
		Example: `  rc paywalls create --offering-id ofrng_default
  rc paywalls create --offering-id ofrng_default --json --no-input`,
		Args: cobra.NoArgs,
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
			offeringID, err = requireID(rt, offeringID, "offering", func() ([]PickerItem, error) {
				return offeringPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			paywall, err := client.Paywalls.Create(cmd.Context(), projectID, api.PaywallCreate{
				OfferingID:                 offeringID,
				AutomaticallyScaleFontSize: automaticallyScaleFontSize,
			})
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Created draft paywall %s", paywall.ID))
			rt.Out.Info(fmt.Sprintf("Review it, then publish with: rc paywalls publish %s", paywall.ID))
			return rt.Out.Render(paywall)
		},
	}
	cmd.Flags().StringVar(&offeringID, "offering-id", "", "offering to attach (picker shown in TTY if omitted)")
	cmd.Flags().BoolVar(&automaticallyScaleFontSize, "automatically-scale-font-size", true, "automatically scale paywall fonts")
	return cmd
}

func newPaywallsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List paywalls",
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
			page, err := client.Paywalls.List(cmd.Context(), projectID)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(page.Items))
			for _, p := range page.Items {
				published := "—"
				if p.PublishedAt != nil {
					published = formatMillis(int64(*p.PublishedAt))
				}
				rows = append(rows, []string{p.ID, p.OfferingID, formatMillis(int64(p.CreatedAt)), published})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "OFFERING", "CREATED", "PUBLISHED"},
				Rows:    rows,
				Raw:     page,
			})
		},
	}
}

func newPaywallsPublishCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "publish [id]",
		Short: "Publish the current paywall draft",
		Long: `Publishes the current draft and makes its components available to RevenueCat SDKs.

This changes the customer-facing paywall. Review the draft before publishing.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc paywalls publish pw_abc
  rc paywalls publish pw_abc --yes --no-input --json`,
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
			paywallID, err := requireID(rt, argAt(args, 0), "paywall", func() ([]PickerItem, error) {
				return paywallPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			if !rt.Globals.AssumeYes {
				ok, err := tui.Confirm(rt.Globals.NoInput, fmt.Sprintf("Publish paywall %q?", paywallID))
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("aborted")
				}
			}
			paywall, err := client.Paywalls.Publish(cmd.Context(), projectID, paywallID)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Published %s", paywall.ID))
			return rt.Out.Render(paywall)
		},
	}
}

func paywallPickerItems(ctx context.Context, client *api.Client, projectID string) ([]PickerItem, error) {
	page, err := client.Paywalls.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	items := make([]PickerItem, len(page.Items))
	for i, paywall := range page.Items {
		label := paywall.Name
		if label == "" {
			label = paywall.ID
		}
		items[i] = PickerItem{ID: paywall.ID, Label: label}
	}
	return items, nil
}

func formatPublishedAt(publishedAt *api.Millis) string {
	if publishedAt == nil {
		return "draft"
	}
	return formatMillis(int64(*publishedAt))
}

func newPaywallsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show a paywall",
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
			p, err := client.Paywalls.Get(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
			}
			return rt.Out.Render(p)
		},
	}
}

func newPaywallsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a paywall",
		Long: `Permanently deletes a paywall.

Reversibility: irreversible. Recreate the paywall if it is deleted.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc paywalls delete pw_old --yes`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if !rt.Globals.AssumeYes {
				ok, err := tui.Confirm(rt.Globals.NoInput, fmt.Sprintf("Delete paywall %q?", args[0]))
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
			if err := client.Paywalls.Delete(cmd.Context(), projectID, args[0]); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Deleted %s", args[0]))
			return rt.Out.Render(map[string]any{"ok": true, "id": args[0]})
		},
	}
}
