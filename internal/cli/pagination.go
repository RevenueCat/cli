package cli

import (
	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
)

// addListPaginationFlags binds the standard --limit / --cursor flags for a
// cursor-paginated list command.
func addListPaginationFlags(cmd *cobra.Command, opts *api.ListOptions) {
	cmd.Flags().IntVar(&opts.Limit, "limit", 0, "max results per page (server default if unset)")
	cmd.Flags().StringVar(&opts.StartingAfter, "cursor", "", "item ID to start after (pagination)")
}

// hintMoreResults nudges to the next page when one exists (human output only).
func hintMoreResults[T any](rt *Runtime, page *api.Page[T]) {
	if rt.Globals.JSON {
		return
	}
	if cursor := page.NextCursor(); cursor != "" {
		rt.Out.Info("more results — pass --cursor " + cursor + " for the next page")
	}
}
