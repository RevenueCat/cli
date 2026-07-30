package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
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
		newPaywallsGenerateCmd(),
		newPaywallsEditCmd(),
		newPaywallsRewindCmd(),
		newPaywallsPublishCmd(),
		newPaywallsUnpublishCmd(),
		newPaywallsDeleteCmd(),
	)
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
			if err := confirmOrAbort(rt, fmt.Sprintf("Publish paywall %q?", paywallID)); err != nil {
				return err
			}
			paywall, err := client.Paywalls.Publish(cmd.Context(), projectID, paywallID)
			if err != nil {
				return wrapPaywallActionGateError(err, "publish")
			}
			rt.Out.Success(fmt.Sprintf("Published %s", paywall.ID))
			return rt.Out.Render(paywall)
		},
	}
}

func newPaywallsUnpublishCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unpublish [id]",
		Short: "Unpublish a paywall",
		Long: `Removes the published paywall so RevenueCat SDKs stop serving it to customers.

Unpublishing can be blocked when the paywall's offering is used by an active experiment.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc paywalls unpublish pw_abc
  rc paywalls unpublish pw_abc --yes --no-input --json`,
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
			if err := confirmOrAbort(rt, fmt.Sprintf("Unpublish paywall %q?", paywallID)); err != nil {
				return err
			}
			paywall, err := client.Paywalls.Unpublish(cmd.Context(), projectID, paywallID)
			if err != nil {
				return wrapPaywallActionGateError(err, "unpublish")
			}
			rt.Out.Success(fmt.Sprintf("Unpublished %s", paywall.ID))
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
		Use:   "show [id]",
		Short: "Show a paywall",
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
			paywallID, err := requireID(rt, argAt(args, 0), "paywall", func() ([]PickerItem, error) {
				return paywallPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			p, err := client.Paywalls.Get(cmd.Context(), projectID, paywallID)
			if err != nil {
				return err
			}
			return rt.Out.Render(p)
		},
	}
}

func newPaywallsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete [id]",
		Short: "Delete a paywall",
		Long: `Permanently deletes a paywall.

Reversibility: irreversible. Recreate the paywall if it is deleted.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc paywalls delete pw_old --yes`,
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
			pickedID, err := requireID(rt, argAt(args, 0), "paywall", func() ([]PickerItem, error) {
				return paywallPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			args = []string{pickedID}
			if err := confirmOrAbort(rt, fmt.Sprintf("Delete paywall %q?", args[0])); err != nil {
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

// wrapOfferingPaywallExistsError turns the server's bare "already exists" 409
// into the three ways out: another offering, delete the existing paywall, or
// drop the offering and attach later. An offering can only have one paywall.
func wrapOfferingPaywallExistsError(err error, offeringID string) error {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.Status == 409 {
		return fmt.Errorf("%w\noffering %s already has a paywall — an offering can only have one. Generate against another offering, delete the existing paywall (rc paywalls list, then rc paywalls delete <id>), or omit --offering-id to generate a standalone draft and attach it in the dashboard later", err, offeringID)
	}
	return err
}

// wrapPaywallActionGateError explains the beta gate on the paywall
// publish/unpublish v2 actions: they 404 with a bare "Resource not found"
// for projects without beta API access (khepri #22939 proposes ungating).
// Without this hint, agents chase the 404 as a paywall-existence bug.
func wrapPaywallActionGateError(err error, action string) error {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.Status == 404 && strings.Contains(apiErr.Message, "Resource not found") {
		return fmt.Errorf("%w\n%s is currently gated to beta API access — %s this paywall from the RevenueCat dashboard (Paywalls -> open the draft -> %s), or ask for v2 beta API access", err, action, action, action)
	}
	return err
}
