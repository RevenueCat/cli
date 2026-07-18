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

// Webhooks is `rc webhooks` in CLI surface but hits /integrations/webhooks
// on the wire (the bare /integrations URL 404s; sub-type is part of the path).
// User-facing name follows what users say, not the API URL structure.

func newWebhooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "webhooks",
		Aliases: []string{"webhook"},
		Short:   "Manage webhook integrations",
	}
	cmd.AddCommand(
		newWebhooksListCmd(),
		newWebhooksShowCmd(),
		newWebhooksCreateCmd(),
		newWebhooksUpdateCmd(),
		newWebhooksDeleteCmd(),
	)
	return cmd
}

func newWebhooksListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List webhooks",
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
			page, err := client.Webhooks.List(cmd.Context(), projectID)
			if err != nil {
				return err
			}

			if !rt.Globals.JSON && !rt.Globals.NoInput && tui.IsInteractive() {
				items := make([]tui.BrowserItem, len(page.Items))
				for i, w := range page.Items {
					items[i] = webhookToItem(projectID, w)
				}
				return tui.RunBrowserTable("Webhooks", []string{"ID", "URL", "STATUS"}, items)
			}

			rows := make([][]string, 0, len(page.Items))
			for _, w := range page.Items {
				rows = append(rows, []string{w.ID, w.URL, w.Status, formatMillis(int64(w.CreatedAt))})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "URL", "STATUS", "CREATED"},
				Rows:    rows,
				Raw:     page,
			})
		},
	}
}

// ── browser helpers ──────────────────────────────────────────────────────────

func webhookToItem(projectID string, w api.Webhook) tui.BrowserItem {
	return tui.BrowserItem{
		ID:     w.ID,
		Label:  w.URL,
		Meta:   w.Status,
		Row:    []string{w.ID, w.URL, w.Status},
		WebURL: fmt.Sprintf("https://app.revenuecat.com/projects/%s/integrations", dashboardProjectID(projectID)),
		Fields: []tui.BrowserField{
			{Key: "ID", Value: w.ID},
			{Key: "URL", Value: w.URL},
			{Key: "Status", Value: w.Status},
			{Key: "Created", Value: formatMillis(int64(w.CreatedAt))},
		},
	}
}

func newWebhooksShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show [id]",
		Short: "Show a webhook",
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
			webhookID, err := requireID(rt, argAt(args, 0), "webhook", func() ([]PickerItem, error) {
				return webhookPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			w, err := client.Webhooks.Get(cmd.Context(), projectID, webhookID)
			if err != nil {
				return err
			}
			if !rt.Globals.JSON && !rt.Globals.NoInput && tui.IsInteractive() {
				item := webhookToItem(projectID, *w)
				return tui.RunBrowser("Webhook", []tui.BrowserItem{item})
			}
			return rt.Out.Render(w)
		},
	}
}

func newWebhooksCreateCmd() *cobra.Command {
	var urlStr string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a webhook",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if err := tui.Form(rt.Globals.NoInput).
				Field(huh.NewInput().Title("Webhook URL").Value(&urlStr).Validate(tui.Required("URL"))).
				Run(); err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			w, err := client.Webhooks.Create(cmd.Context(), projectID, api.WebhookCreate{URL: urlStr})
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Created webhook %s", w.ID))
			return rt.Out.Render(w)
		},
	}
	cmd.Flags().StringVar(&urlStr, "url", "", "webhook URL (required)")
	return cmd
}

func newWebhooksUpdateCmd() *cobra.Command {
	var urlStr, status string
	cmd := &cobra.Command{
		Use:   "update [id]",
		Short: "Update a webhook",
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
			webhookID, err := requireID(rt, argAt(args, 0), "webhook", func() ([]PickerItem, error) {
				return webhookPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			body := api.WebhookUpdate{}
			if cmd.Flags().Changed("url") {
				body.URL = &urlStr
			}
			if cmd.Flags().Changed("status") {
				body.Status = &status
			}
			w, err := client.Webhooks.Update(cmd.Context(), projectID, webhookID, body)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Updated %s", w.ID))
			return rt.Out.Render(w)
		},
	}
	cmd.Flags().StringVar(&urlStr, "url", "", "new URL")
	cmd.Flags().StringVar(&status, "status", "", "new status (active|paused)")
	return cmd
}

func newWebhooksDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete [id]",
		Short: "Delete a webhook",
		Long: `Permanently deletes a webhook integration. Future events stop being
delivered to the configured URL.

Reversibility: irreversible. To temporarily disable delivery without
deleting, prefer ` + "`rc webhooks update <id> --status paused`" + `.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc webhooks delete wh_old --yes`,
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
			webhookID, err := requireID(rt, argAt(args, 0), "webhook", func() ([]PickerItem, error) {
				return webhookPickerItems(cmd.Context(), client, projectID)
			})
			if err != nil {
				return err
			}
			if err := confirmOrAbort(rt, fmt.Sprintf("Delete webhook %q?", webhookID)); err != nil {
				return err
			}
			if err := client.Webhooks.Delete(cmd.Context(), projectID, webhookID); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Deleted %s", webhookID))
			return rt.Out.Render(map[string]any{"ok": true, "id": webhookID})
		},
	}
}

// ── picker helpers ───────────────────────────────────────────────────────────

func webhookPickerItems(ctx context.Context, client *api.Client, projectID string) ([]PickerItem, error) {
	page, err := client.Webhooks.List(ctx, projectID)
	if err != nil {
		return nil, err
	}
	items := make([]PickerItem, len(page.Items))
	for i, w := range page.Items {
		items[i] = PickerItem{ID: w.ID, Label: fmt.Sprintf("%s  (%s)", w.URL, w.Status)}
	}
	return items, nil
}
