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
		Short:   "Create and inspect Paywalls",
		Long: `Design, iterate on, and publish paywalls.

The design workflow: generate a draft with rc paywalls generate, look at the
preview screenshot it writes, iterate with rc paywalls edit on the SAME
--session, and run rc paywalls publish only after the user reviewed and
approved the design. See generate's and edit's help for how to prompt well.

Iterating is the normal workflow: expect several edit turns until the result
is acceptable, not a perfect first try. Every completed turn writes a preview
screenshot; judge it against the direction and keep editing until it matches.
Undo a bad turn with rc paywalls rewind.

Custom assets: upload the app's real fonts with rc fonts upload and real
images (logo, hero) with rc media-assets upload BEFORE designing, then
reference them in prompts — see those commands' help for the exact wiring.

Publishing is customer-facing: review the preview screenshot and the builder
URL, get the user's approval, then rc paywalls publish.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			if !guidedMode() || !rt.CanPrompt() {
				return cmd.Help()
			}
			if err := ensureAuthInteractive(cmd); err != nil {
				return err
			}
			action, err := decide(rt, "What would you like to do?", nil, []Choice[string]{
				{Value: "generate", Label: "Generate a paywall with AI", Flag: "rc paywalls generate"},
				{Value: "edit", Label: "Edit an existing paywall with AI", Flag: "rc paywalls edit"},
			})
			if err != nil {
				return err
			}
			for _, sub := range cmd.Commands() {
				if sub.Name() == action {
					sub.SetContext(cmd.Context())
					return sub.RunE(sub, nil)
				}
			}
			return cmd.Help()
		},
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

// ensureAuthInteractive runs the login flow when no credential is stored yet.
func ensureAuthInteractive(cmd *cobra.Command) error {
	rt := RuntimeFrom(cmd.Context())
	if rt.Config.BearerToken() != "" {
		return nil
	}
	rt.Out.Hint("You're not logged in yet — let's do that first.")
	login, _, err := cmd.Root().Find([]string{"auth", "login"})
	if err != nil || login == nil {
		return ErrNotAuthenticated
	}
	login.SetContext(cmd.Context())
	return login.RunE(login, nil)
}

func newPaywallsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List Paywalls",
		Example: `  rc paywalls list
  rc paywalls list --json | jq '.data.items[].id'`,
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
		Short: "Publish the current Paywall draft",
		Long: `Publishes the current draft and makes its components available to RevenueCat SDKs.

This changes what customers see. Review the preview screenshot and the
dashboard builder URL, and get the user's approval before publishing.

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
		Short: "Unpublish a Paywall",
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
		items[i] = PickerItem{ID: paywall.ID, Label: paywallPickerLabel(paywall)}
	}
	return items, nil
}

// paywallPickerLabel folds offering, date, and publish state into the label:
// paywall names are usually the default "Untitled Paywall", so the name alone
// can't tell rows apart.
func paywallPickerLabel(p api.Paywall) string {
	name := p.Name
	if name == "" {
		name = p.ID
	}
	offering := "standalone"
	if p.OfferingID != "" {
		offering = p.OfferingID
	}
	status := "draft"
	if p.PublishedAt != nil {
		status = "published"
	}
	return fmt.Sprintf("%s · %s · %s · %s", name, offering, formatMillis(int64(p.CreatedAt)), status)
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
		Short: "Show a Paywall",
		Long: `Prints paywall metadata: offering, timestamps, publish state.

For design work, look at the session preview screenshot and the dashboard
builder URL instead — show returns metadata, not visuals.`,
		Example: `  rc paywalls show pw_abc
  rc paywalls show pw_abc --json`,
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
			p, err := client.Paywalls.Get(cmd.Context(), projectID, paywallID)
			if err != nil {
				return err
			}
			return rt.Out.Render(p)
		},
	}
}

func newPaywallsDeleteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "delete [id]",
		Short: "Delete a Paywall",
		Long: `Permanently deletes a paywall.

Reversibility: irreversible. Recreate the paywall if it is deleted.

A paywall that is attached to an offering or published refuses to delete
unless --force is passed — --yes alone is not enough. It may be live or
someone else's in-progress work; get explicit consent from the user first.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc paywalls delete pw_old --yes
  rc paywalls delete pw_attached --force --yes`,
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
			pickedID, err := requireID(rt, argAt(args, 0), "paywall", func() ([]PickerItem, error) {
				return paywallPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			paywall, err := client.Paywalls.Get(cmd.Context(), projectID, pickedID)
			if err != nil {
				return err
			}
			if !force {
				switch {
				case paywall.PublishedAt != nil:
					return WithHint(
						fmt.Errorf("paywall %s is published — customers may be seeing it", pickedID),
						"Deletion is irreversible. Unpublish it first (rc paywalls unpublish "+pickedID+") and detach it from its offering in the dashboard, or re-run with --force after the user explicitly confirms this paywall should be destroyed.",
					)
				case paywall.OfferingID != "":
					return WithHint(
						fmt.Errorf("paywall %s is attached to offering %s and may be someone's in-progress work", pickedID, paywall.OfferingID),
						"Deletion is irreversible. Re-run with --force only after the user explicitly confirms this paywall should be destroyed. To free the offering for a new design instead, generate a standalone draft (rc paywalls generate without --offering-id) and attach it in the dashboard.",
					)
				}
			}
			if paywall.PublishedAt != nil {
				rt.Out.Warn("This paywall is published — customers may be seeing it.")
			}
			if paywall.OfferingID != "" {
				rt.Out.Warn(fmt.Sprintf("Attached to offering %s.", paywall.OfferingID))
			}
			if err := confirmOrAbort(rt, fmt.Sprintf("Permanently delete paywall %s?", paywallPickerLabel(*paywall))); err != nil {
				return err
			}
			if err := client.Paywalls.Delete(cmd.Context(), projectID, pickedID); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Deleted %s", pickedID))
			return rt.Out.Render(map[string]any{"ok": true, "id": pickedID})
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "delete even when the paywall is attached to an offering or published")
	return cmd
}

// wrapPaywallActionGateError explains the beta gate on the paywall
// publish/unpublish v2 actions: they 404 with a bare "Resource not found"
// for projects without beta API access.
// Without this hint, agents chase the 404 as a paywall-existence bug.
func wrapPaywallActionGateError(err error, action string) error {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) && apiErr.Status == 404 && strings.Contains(apiErr.Message, "Resource not found") {
		return fmt.Errorf("%w\n%s is currently gated to beta API access — %s this paywall from the RevenueCat dashboard (Paywalls -> open the draft -> %s), or ask for v2 beta API access", err, action, action, action)
	}
	return err
}
