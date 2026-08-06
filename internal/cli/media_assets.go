package cli

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
)

// Accepted upload types per the v2 media_assets endpoint's content_type enum.
// Deliberately not paywallAIImageTypes: that map is editor-specific and lacks
// avif/heic/heif.
var mediaAssetContentTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".avif": "image/avif",
	".heic": "image/heic",
	".heif": "image/heif",
}

// mediaAssetMaxBytes mirrors the endpoint's file_data_base64 maxLength of
// 2,796,204 chars, which is base64.EncodedLen of exactly 2 MiB.
const mediaAssetMaxBytes = 2 << 20

func loadMediaAsset(path string) (api.MediaAssetCreate, error) {
	contentType, ok := mediaAssetContentTypes[strings.ToLower(filepath.Ext(path))]
	if !ok {
		return api.MediaAssetCreate{}, fmt.Errorf("unsupported image type %q (accepted: jpg, jpeg, png, webp, avif, heic, heif)", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return api.MediaAssetCreate{}, err
	}
	if len(data) == 0 {
		return api.MediaAssetCreate{}, fmt.Errorf("image %s is empty", path)
	}
	if len(data) > mediaAssetMaxBytes {
		return api.MediaAssetCreate{}, fmt.Errorf("image %s is %.1f MiB; the upload limit is 2 MiB — resize or compress it first", path, float64(len(data))/(1<<20))
	}
	return api.MediaAssetCreate{
		Filename:       filepath.Base(path),
		ContentType:    contentType,
		FileDataBase64: base64.StdEncoding.EncodeToString(data),
	}, nil
}

func newMediaAssetsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "media-assets",
		Short: "Manage project media assets",
	}
	cmd.AddCommand(newMediaAssetsUploadCmd())
	return cmd
}

func newMediaAssetsUploadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upload <file>",
		Short: "Upload an image to the project Media Gallery",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			body, err := loadMediaAsset(args[0])
			if err != nil {
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
			// Paywall components store the full URL, not the bare object name.
			if asset.AssetBaseURL != nil {
				rt.Out.Hint("Reference it from paywall components as " + *asset.AssetBaseURL + "/" + asset.ObjectName)
			}
			return rt.Out.Render(asset)
		},
	}
}
