package cli

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
	"github.com/revenuecat/cli/internal/output"
)

var mediaAssetContentTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".avif": "image/avif",
	".heic": "image/heic",
	".heif": "image/heif",
}

const mediaAssetMaxBytes = 2 << 20

var errMediaAssetTooLarge = errors.New("the upload limit is 2 MiB")

func loadMediaAsset(path string) (api.CreateMediaAssetJSONBody, error) {
	contentType, ok := mediaAssetContentTypes[strings.ToLower(filepath.Ext(path))]
	if !ok {
		return api.CreateMediaAssetJSONBody{}, fmt.Errorf("unsupported image type %q (accepted: jpg, jpeg, png, webp, avif, heic, heif)", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return api.CreateMediaAssetJSONBody{}, err
	}
	if len(data) == 0 {
		return api.CreateMediaAssetJSONBody{}, fmt.Errorf("image %s is empty", path)
	}
	if len(data) > mediaAssetMaxBytes {
		return api.CreateMediaAssetJSONBody{}, fmt.Errorf("image %s is %d KB: %w", path, len(data)/1024, errMediaAssetTooLarge)
	}
	return api.CreateMediaAssetJSONBody{
		Filename:       filepath.Base(path),
		ContentType:    api.CreateMediaAssetJSONBodyContentType(contentType),
		FileDataBase64: base64.StdEncoding.EncodeToString(data),
	}, nil
}

func newMediaAssetsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "media-assets",
		Short: "Manage project images for paywalls (Media Gallery)",
	}
	cmd.AddCommand(newMediaAssetsUploadCmd(), newMediaAssetsListCmd())
	return cmd
}

func newMediaAssetsListCmd() *cobra.Command {
	var limit int
	var cursor string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List media assets in the project Media Gallery",
		Example: `  rc media-assets list
  rc media-assets list --json | jq '.data.items[].id'`,
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
			page, err := client.MediaAssets.List(cmd.Context(), projectID, &api.ListMediaAssetsOptions{
				Limit:         limit,
				StartingAfter: cursor,
			})
			if err != nil {
				return err
			}

			rows := make([][]string, 0, len(page.Items))
			for _, a := range page.Items {
				dimensions := ""
				if a.OriginalWidth != nil && a.OriginalHeight != nil {
					dimensions = fmt.Sprintf("%dx%d", *a.OriginalWidth, *a.OriginalHeight)
				}
				reference := a.ObjectName
				if a.AssetBaseURL != nil {
					reference = *a.AssetBaseURL + "/" + a.ObjectName
				}
				rows = append(rows, []string{a.ID, a.OriginalName, dimensions, string(a.AssetType), reference})
			}
			if err := rt.Out.RenderTable(output.Table{
				Columns: []string{"ID", "NAME", "DIMENSIONS", "TYPE", "REFERENCE"},
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
	cmd.Flags().StringVar(&cursor, "cursor", "", "media asset ID to start after (pagination)")
	return cmd
}

func newMediaAssetsUploadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upload <file>",
		Short: "Upload an image to the project Media Gallery",
		Long: `Uploads an image to the project Media Gallery for use in paywalls.

Compose the reference URL as asset_base_url + "/" + object_name from the
upload response. Per-rendition URLs come from formats.<name>.object_name.
Reuse the original URL for any rendition missing from the response.

An image component's source needs exactly these fields — all required, no
extra keys, https URLs from the upload response only:

  "source": {"light": {"width", "height", "original", "webp",
             "webp_low_res", "heic", "heic_low_res"}}

A background image takes this form:

  {"type": "image", "value": {"light": {…}}, "fit_mode": "fill"}

Two routes to place it: paste the URL into an rc paywalls edit prompt for the
editor to place it, or PATCH the components directly (see rc paywalls --help
for the recipe).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			body, err := loadMediaAsset(args[0])
			if err != nil {
				if errors.Is(err, errMediaAssetTooLarge) {
					rt.Out.Hint("Resize or compress the image to 2 MiB or less, then retry")
				}
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			asset, err := client.MediaAssets.Create(cmd.Context(), projectID, body)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Uploaded %s (%s, %d KB)", asset.ID, asset.OriginalName, asset.OriginalSize))
			if asset.AssetBaseURL != nil {
				rt.Out.Hint("Reference it from paywall components as " + *asset.AssetBaseURL + "/" + asset.ObjectName)
			}
			return rt.Out.Render(asset)
		},
	}
}
