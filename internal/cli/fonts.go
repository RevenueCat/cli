package cli

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
)

var fontContentTypes = map[string]string{
	".ttf": "font/ttf",
	".otf": "font/otf",
}

const fontMaxBytes = 5 << 20

var errFontTooLarge = errors.New("the upload limit is 5 MiB")

func loadFont(path string) (api.FontCreate, error) {
	contentType, ok := fontContentTypes[strings.ToLower(filepath.Ext(path))]
	if !ok {
		return api.FontCreate{}, fmt.Errorf("unsupported font type %q (accepted: ttf, otf)", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return api.FontCreate{}, err
	}
	if len(data) == 0 {
		return api.FontCreate{}, fmt.Errorf("font %s is empty", path)
	}
	if len(data) > fontMaxBytes {
		return api.FontCreate{}, fmt.Errorf("font %s is %d KB: %w", path, len(data)/1024, errFontTooLarge)
	}
	return api.FontCreate{
		Filename:       filepath.Base(path),
		ContentType:    contentType,
		FileDataBase64: base64.StdEncoding.EncodeToString(data),
	}, nil
}

func newFontsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fonts",
		Short: "Manage project fonts",
	}
	cmd.AddCommand(newFontsUploadCmd(), newFontsListCmd())
	return cmd
}

func newFontsListCmd() *cobra.Command {
	var limit int
	var cursor string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List fonts uploaded to the project",
		Example: `  rc fonts list
  rc fonts list --json | jq '.data.items[].font_key'`,
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
			page, err := client.Fonts.List(cmd.Context(), projectID, &api.ListFontsOptions{
				Limit:         limit,
				StartingAfter: cursor,
			})
			if err != nil {
				return err
			}

			rows := make([][]string, 0, len(page.Items))
			for _, f := range page.Items {
				rows = append(rows, []string{f.ID, f.Name, f.FamilyName, f.Style, strconv.Itoa(f.Weight), f.FontKey})
			}
			if err := rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "NAME", "FAMILY", "STYLE", "WEIGHT", "FONT KEY"},
				Rows:    rows,
				Raw:     page,
			}); err != nil {
				return err
			}
			if page.NextPage != "" && !rt.Globals.JSON && len(page.Items) > 0 {
				rt.Out.Info(fmt.Sprintf("more results — pass --cursor %s for the next page", page.Items[len(page.Items)-1].ID))
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "max results per page (server default if unset)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "font ID to start after (pagination)")
	return cmd
}

func newFontsUploadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upload <file>",
		Short: "Upload a font to the project for use in paywalls",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			body, err := loadFont(args[0])
			if err != nil {
				if errors.Is(err, errFontTooLarge) {
					rt.Out.Hint("Use a font file of 5 MiB or less, then retry")
				}
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			font, err := client.Fonts.Create(cmd.Context(), projectID, body)
			if err != nil {
				var apiErr *api.APIError
				if errors.As(err, &apiErr) && apiErr.Status == 409 {
					rt.Out.Hint("This font style is already uploaded — run `rc fonts list` to find it and reference it by its font key")
				}
				return err
			}
			rt.Out.Success(fmt.Sprintf("Uploaded %s (%s, %s %d)", font.ID, font.Name, font.Style, font.Weight))
			rt.Out.Hint("Reference it from paywall components as " + font.FontKey)
			return rt.Out.Render(font)
		},
	}
}
