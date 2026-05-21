package cli

import (
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
					items[i] = tui.BrowserItem{
						ID:     w.ID,
						Label:  w.URL,
						Meta:   w.Status,
						WebURL: fmt.Sprintf("https://app.revenuecat.com/projects/%s/integrations", dashboardProjectID(projectID)),
						Fields: []tui.BrowserField{
							{Key: "ID", Value: w.ID},
							{Key: "URL", Value: w.URL},
							{Key: "Status", Value: w.Status},
							{Key: "Created", Value: formatMillis(w.CreatedAt)},
						},
					}
				}
				return tui.RunBrowser("Webhooks", items)
			}

			rows := make([][]string, 0, len(page.Items))
			for _, w := range page.Items {
				rows = append(rows, []string{w.ID, w.URL, w.Status, formatMillis(w.CreatedAt)})
			}
			return rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "URL", "STATUS", "CREATED"},
				Rows:    rows,
				Raw:     page,
			})
		},
	}
}

func newWebhooksShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show a webhook",
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
			w, err := client.Webhooks.Get(cmd.Context(), projectID, args[0])
			if err != nil {
				return err
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
		Use:   "update <id>",
		Short: "Update a webhook",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
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
			client, err := rt.API()
			if err != nil {
				return err
			}
			w, err := client.Webhooks.Update(cmd.Context(), projectID, args[0], body)
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
		Use:   "delete <id>",
		Short: "Delete a webhook",
		Long: `Permanently deletes a webhook integration. Future events stop being
delivered to the configured URL.

Reversibility: irreversible. To temporarily disable delivery without
deleting, prefer ` + "`rc webhooks update <id> --status paused`" + `.

Confirmation: prompts under TTY; pass --yes to skip. Required under --no-input.`,
		Example: `  rc webhooks delete wh_old --yes`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			if !rt.Globals.AssumeYes {
				ok, err := tui.Confirm(rt.Globals.NoInput, fmt.Sprintf("Delete webhook %q?", args[0]))
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
			if err := client.Webhooks.Delete(cmd.Context(), projectID, args[0]); err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Deleted %s", args[0]))
			return rt.Out.Render(map[string]any{"ok": true, "id": args[0]})
		},
	}
}
